// marmot-backend: compiles MLIR modules produced by the marmot frontend to
// object files. See SPEC.md section 2.3.
//
//   marmot-backend [-O2] [-emit=obj|asm|llvm|bc|mlir] [-rt-bitcode path] -o out in.mlir
//   marmot-backend -batch jobs.txt -j N      (one "in out" pair per line)
#include "marmot/Driver/Pipeline.h"
#include "marmot/Driver/RuntimeBitcode.h"
#include "marmot/Driver/Watchdog.h"
#include "marmot/Driver/WholeProgram.h"

#include "llvm/ADT/StringExtras.h"
#include "llvm/Support/CommandLine.h"
#include "llvm/Support/InitLLVM.h"
#include "llvm/Support/MemoryBuffer.h"
#include "llvm/Support/ThreadPool.h"
#include "llvm/Support/Threading.h"
#include "llvm/Support/Format.h"
#include "llvm/Support/raw_ostream.h"

#include <algorithm>
#include <atomic>
#include <cstdlib>
#include <thread>
#include <chrono>
#include <mutex>

using namespace llvm;

static cl::OptionCategory cat("marmot-backend options");

static cl::opt<std::string> inputFile(cl::Positional, cl::desc("<input.mlir>"),
                                      cl::cat(cat));
static cl::opt<std::string> outputFile("o", cl::desc("Output file"),
                                       cl::value_desc("path"), cl::cat(cat));
static cl::opt<std::string> optLevel("O", cl::desc("Optimization level: 0,1,2,3,s,z"),
                                     cl::Prefix, cl::init("2"), cl::cat(cat));
static cl::opt<marmot::Emit> emitKind(
    "emit", cl::desc("Output kind"), cl::init(marmot::Emit::Obj),
    cl::values(clEnumValN(marmot::Emit::Obj, "obj", "object file"),
               clEnumValN(marmot::Emit::Asm, "asm", "assembly"),
               clEnumValN(marmot::Emit::LLVM, "llvm", "LLVM IR text"),
               clEnumValN(marmot::Emit::BC, "bc", "LLVM bitcode"),
               clEnumValN(marmot::Emit::MLIR, "mlir", "MLIR after lowering")),
    cl::cat(cat));
static cl::opt<std::string> rtBitcode("rt-bitcode",
                                      cl::desc("Runtime core bitcode to import from"),
                                      cl::value_desc("path"), cl::cat(cat));
static cl::opt<std::string> targetTriple("target", cl::desc("Target triple (default: host)"),
                                         cl::cat(cat));
static cl::opt<std::string> targetCPU("mcpu", cl::desc("Target CPU (default: x86-64-v2 on x86-64, the host CPU otherwise; native: the host CPU and its features)"),
                                      cl::cat(cat));
static cl::opt<std::string> targetAttrs("mattr", cl::desc("Target features"),
                                        cl::cat(cat));
static cl::opt<std::string> batchFile("batch",
                                      cl::desc("File with one \"in out\" pair per line"),
                                      cl::value_desc("path"), cl::cat(cat));
static cl::opt<unsigned> jobs("j", cl::desc("Parallel jobs in batch mode (0: all cores, "
                                             "capped by MARMOT_MAX_JOBS, default 4)"),
                              cl::init(0), cl::cat(cat));
static cl::opt<bool> timePhases("time", cl::desc("Report time per phase"), cl::cat(cat));
static cl::opt<bool> initOptNone(
    "init-optnone", cl::init(true),
    cl::desc("Do not optimize Go package init functions (they run once)"), cl::cat(cat));
static cl::opt<bool> noEscape("no-escape", cl::desc("Disable the go-escape pass"),
                              cl::cat(cat));
static cl::opt<bool> noDevirt("no-devirt", cl::desc("Disable the go-devirt pass"),
                              cl::cat(cat));
static cl::opt<bool> noOpenDefers("no-open-defers",
                                  cl::desc("Disable the go-open-defers pass"), cl::cat(cat));
static cl::opt<bool> noStrings("no-strings", cl::desc("Disable the go-strings pass"),
                               cl::cat(cat));
static cl::opt<bool> noBoolPack("no-bool-pack", cl::desc("Disable the go-bool-pack pass"),
                                cl::cat(cat));
static cl::opt<bool> noBCE("no-bce", cl::desc("Disable the go-bce pass"), cl::cat(cat));
static cl::opt<bool> wpo("wpo",
                         cl::desc("Batch mode: compile the jobs as one program (drop "
                                  "unreachable code, internalize, import across modules)"),
                         cl::cat(cat));
static cl::opt<std::string> cacheDir(
    "cache-dir", cl::desc("Batch mode: cache directory for summaries, bitcode and objects"),
    cl::value_desc("dir"), cl::cat(cat));
static cl::opt<bool> noClosedWorld(
    "no-closed-world",
    cl::desc("-wpo: keep the indirect fallback of devirtualized calls"), cl::cat(cat));
static cl::opt<bool> noPruneTypes(
    "no-prune-types", cl::desc("-wpo: keep every type's fields and methods tables"),
    cl::cat(cat));
static cl::opt<bool> noImport("no-import", cl::desc("-wpo without cross-module imports"),
                              cl::cat(cat));
static cl::opt<bool> noLoops("no-loops", cl::desc("Disable the loop optimizer"), cl::cat(cat));
static cl::opt<bool> noPar("no-par", cl::desc("Loop optimizer: no parallel-for (the default)"),
                           cl::cat(cat));
// Parallel-for is opt-in until the miscompiles in parallelized versioned
// loops (tests/run/opt/sort_exact, std/exec/run, os.Environ) are fixed.
static cl::opt<bool> par("par", cl::desc("Loop optimizer: parallel-for (opt-in for now)"),
                         cl::cat(cat));
static cl::opt<bool> fastFP("ffast-fp",
                            cl::desc("Loop optimizer: allow reordering floating point "
                                     "reductions (changes results; opt-in)"),
                            cl::cat(cat));
static cl::opt<bool> loopsRemarks("loops-remarks",
                                  cl::desc("Loop optimizer: explain each loop decision"),
                                  cl::cat(cat));
static cl::opt<bool> noBCV("no-bcv", cl::desc("Loop optimizer: no bounds check versioning"),
                           cl::cat(cat));
static cl::opt<bool> typeAlias("type-alias",
                               cl::desc("Loop optimizer: alias metadata from Go type classes"),
                               cl::cat(cat));
static cl::opt<std::string> pgoUse("pgo",
                                   cl::desc("Optimize with this indexed profile "
                                            "(llvm-profdata merge -o FILE *.profraw)"),
                                   cl::value_desc("file"), cl::cat(cat));
static cl::opt<std::string> pgoGen(
    "pgo-gen", cl::ValueOptional,
    cl::desc("Instrument for profiling; the program writes PATTERN at exit "
             "(default default_%m.profraw)"),
    cl::value_desc("pattern"), cl::cat(cat));
static cl::opt<std::string> gpuArch(
    "gpu", cl::ValueOptional,
    cl::desc("Offload marked parallel loops to an NVIDIA GPU (PTX for ARCH, default sm_86)"),
    cl::value_desc("arch"), cl::cat(cat));
static cl::opt<bool> keepTailCalls(
    "keep-tail-calls",
    cl::desc("Allow sibling calls in Go code with -g (frames then go missing from tracebacks)"));
static cl::opt<int> debugLevel(
    "g", cl::desc("Debug info from source locations: 0 none, 1 line tables (default), 2 full"),
    cl::init(1), cl::cat(cat));
static cl::opt<bool> prepareOnly(
    "prepare-only",
    cl::desc("Batch with -cache-dir: fill the cache (with -wpo: only the program "
             "independent first phase) and write no outputs"),
    cl::cat(cat));
static cl::opt<bool> verifyEach("verify-each",
                                cl::desc("Verify IR after every pass"), cl::cat(cat));

namespace {

struct Job {
  std::string in, out;
};

bool readJobs(const std::string &path, std::vector<Job> &out, std::string &err) {
  auto buf = MemoryBuffer::getFileOrSTDIN(path);
  if (!buf) {
    err = path + ": " + buf.getError().message();
    return false;
  }
  SmallVector<StringRef, 0> lines;
  (*buf)->getBuffer().split(lines, '\n');
  unsigned lineNo = 0;
  for (StringRef line : lines) {
    ++lineNo;
    line = line.trim();
    if (line.empty() || line.starts_with("#"))
      continue;
    SmallVector<StringRef, 2> fields;
    SplitString(line, fields);
    if (fields.size() != 2) {
      err = path + ":" + std::to_string(lineNo) + ": want \"in out\"";
      return false;
    }
    out.push_back({fields[0].str(), fields[1].str()});
  }
  return true;
}

} // namespace

int main(int argc, char **argv) {
  InitLLVM init(argc, argv);
  marmot::startWatchdog();
  cl::HideUnrelatedOptions(cat);
  // Extra options from MARMOT_BACKEND_FLAGS, for example
  // MARMOT_BACKEND_FLAGS="-no-escape -no-bce" through the marmot driver.
  cl::ParseCommandLineOptions(argc, argv, "marmot MLIR backend\n", nullptr, nullptr,
                              "MARMOT_BACKEND_FLAGS");
  auto start = std::chrono::steady_clock::now();

  marmot::CompileOptions opts;
  if (!marmot::parseOptLevel(optLevel, opts.optLevel)) {
    errs() << "marmot-backend: bad -O level: " << optLevel << "\n";
    return 1;
  }
  opts.emit = emitKind;
  opts.triple = targetTriple;
  opts.cpu = targetCPU;
  opts.features = targetAttrs;
  opts.verifyEach = verifyEach;
  opts.initOptNone = initOptNone;
  opts.wpoImport = !noImport;
  opts.wpoClosedWorld = !noClosedWorld;
  opts.wpoPruneTypes = !noPruneTypes;
  opts.pgoUse = pgoUse;
  opts.debugLevel = debugLevel;
  opts.keepTailCalls = keepTailCalls;
  opts.prepareOnly = prepareOnly;
  if (prepareOnly && (batchFile.empty() || cacheDir.empty())) {
    errs() << "marmot-backend: -prepare-only needs -batch and -cache-dir\n";
    return 1;
  }
  if (gpuArch.getNumOccurrences())
    opts.gpuArch = gpuArch.empty() ? "sm_86" : std::string(gpuArch);
  opts.loops.gpu = !opts.gpuArch.empty();

  if (pgoGen.getNumOccurrences())
    opts.pgoGen = pgoGen.empty() ? "default_%m.profraw" : std::string(pgoGen);
  if (!opts.pgoUse.empty() && !opts.pgoGen.empty()) {
    errs() << "marmot-backend: -pgo and -pgo-gen are exclusive\n";
    return 1;
  }
  opts.loops.enable = !noLoops;
  // GPU loops come from the parallel-loop analysis (their CPU fallback is
  // rt_parallel_for), so -gpu turns it on too.
  opts.loops.parallel = (par || gpuArch.getNumOccurrences()) && !noPar;
  opts.loops.fastFP = fastFP;
  opts.loops.remarks = loopsRemarks;
  opts.loops.versioning = !noBCV;
  opts.loops.typeAlias = typeAlias;
  opts.escape = !noEscape;
  opts.bce = !noBCE;
  opts.boolPack = !noBoolPack;
  opts.devirt = !noDevirt;
  opts.strings = !noStrings;
  opts.openDefers = !noOpenDefers;
  opts.timePhases = timePhases;

  // Prepared on first use: a fully cached batch never parses it.
  std::unique_ptr<marmot::LazyRuntimeLib> rt;
  if (!rtBitcode.empty() && opts.optLevel != '0' && emitKind != marmot::Emit::MLIR) {
    rt = std::make_unique<marmot::LazyRuntimeLib>(rtBitcode);
    opts.rtBitcode = rt.get();
  }
  double prepMs = std::chrono::duration<double, std::milli>(
                      std::chrono::steady_clock::now() - start).count();

  std::vector<Job> work;
  if (!batchFile.empty()) {
    if (!inputFile.empty() || !outputFile.empty()) {
      errs() << "marmot-backend: -batch does not take an input or -o\n";
      return 1;
    }
    std::string err;
    if (!readJobs(batchFile, work, err)) {
      errs() << "marmot-backend: " << err << "\n";
      return 1;
    }
  } else {
    if (inputFile.empty() || outputFile.empty()) {
      errs() << "marmot-backend: need an input file and -o (or -batch)\n";
      return 1;
    }
    work.push_back({inputFile, outputFile});
  }

  std::mutex mu;
  std::atomic<bool> ok{true};
  marmot::PhaseTimes total;
  auto runJob = [&](const Job &job, bool singleThreaded) {
    marmot::CompileOptions o = opts;
    o.singleThreaded = singleThreaded;
    std::string msgs;
    raw_string_ostream es(msgs);
    marmot::PhaseTimes t;
    marmot::watchdogBegin(job.in);
    bool good = marmot::compileFile(job.in, job.out, o, t, es);
    marmot::watchdogEnd(job.in);
    std::lock_guard<std::mutex> lock(mu);
    if (!good)
      ok = false;
    errs() << msgs;
    if (opts.timePhases && work.size() > 1)
      t.print(errs(), job.in);
    total.add(t);
  };

  // SPEC rule 10: at most MARMOT_MAX_JOBS (default 4) threads, whatever -j says.
  unsigned cap = 4;
  if (const char *e = std::getenv("MARMOT_MAX_JOBS"))
    if (unsigned v = std::strtoul(e, nullptr, 10))
      cap = v;
  unsigned n = jobs ? unsigned(jobs) : std::thread::hardware_concurrency();
  n = std::max(1u, std::min({n, cap, unsigned(work.size())}));

  // Batch with more than one job (or -wpo or a cache): summaries cross
  // modules. Same objects as compiling each job alone, plus escape facts.
  if (wpo || (!batchFile.empty() && (work.size() > 1 || !cacheDir.empty()))) {
    if (emitKind == marmot::Emit::MLIR) {
      errs() << "marmot-backend: batch mode needs -emit=obj, asm, llvm or bc\n";
      return 1;
    }
    std::vector<marmot::WpoJob> wjobs;
    for (const Job &j : work)
      wjobs.push_back({j.in, j.out});
    marmot::CompileOptions o = opts;
    o.singleThreaded = true;
    marmot::WpoStats st;
    ok = marmot::compileWholeProgram(wjobs, o, n, wpo, cacheDir, total, st, errs());
    if (opts.timePhases)
      errs() << format("batch: %u functions, %u live, %u internalized, %u imported, "
                       "%u no-escape params after %u escape rounds, %u calls devirtualized, "
                       "%u types without reflection tables, "
                       "cache %u hits %u misses\n",
                       st.functions, st.liveFunctions, st.internalized, st.imported,
                       st.noEscapeParams, st.escapeRounds, st.devirtualized.load(),
                       st.prunedTypes,
                       st.cacheHits, st.cacheMisses);
  } else if (work.size() == 1) {
    runJob(work[0], false);
  } else {
    DefaultThreadPool pool(hardware_concurrency(n));
    for (const Job &job : work)
      pool.async([&, job] { runJob(job, true); });
    pool.wait();
  }

  if (opts.timePhases) {
    double wall = std::chrono::duration<double, std::milli>(
                      std::chrono::steady_clock::now() - start).count();
    total.print(errs(), work.size() > 1 ? "sum of jobs" : work[0].in);
    errs() << format("startup %.2f ms, wall %.2f ms, %zu job(s)\n", prepMs,
                     wall, work.size());
  }
  return ok ? 0 : 1;
}

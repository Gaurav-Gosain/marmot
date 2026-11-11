// marmot-backend: compiles MLIR modules produced by the marmot frontend to
// object files. See SPEC.md section 2.3.
//
//   marmot-backend [-O2] [-emit=obj|asm|llvm|bc|mlir] [-rt-bitcode path] -o out in.mlir
//   marmot-backend -batch jobs.txt -j N      (one "in out" pair per line)
#include "marmot/Driver/Pipeline.h"
#include "marmot/Driver/RuntimeBitcode.h"
#include "marmot/Driver/Watchdog.h"

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
static cl::opt<std::string> targetCPU("mcpu", cl::desc("Target CPU (default: host)"),
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
static cl::opt<bool> noBCE("no-bce", cl::desc("Disable the go-bce pass"), cl::cat(cat));
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
  cl::ParseCommandLineOptions(argc, argv, "marmot MLIR backend\n");
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
  opts.escape = !noEscape;
  opts.bce = !noBCE;
  opts.devirt = !noDevirt;
  opts.timePhases = timePhases;

  marmot::RuntimeLib rt;
  if (!rtBitcode.empty() && opts.optLevel != '0' && emitKind != marmot::Emit::MLIR) {
    std::string err;
    if (!marmot::prepareRuntimeBitcode(rtBitcode, rt, err)) {
      errs() << "marmot-backend: " << err << "\n";
      return 1;
    }
    opts.rtBitcode = &rt;
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

  if (work.size() == 1) {
    runJob(work[0], false);
  } else {
    // SPEC rule 10: at most MARMOT_MAX_JOBS (default 4) threads, whatever -j says.
    unsigned cap = 4;
    if (const char *e = std::getenv("MARMOT_MAX_JOBS"))
      if (unsigned v = std::strtoul(e, nullptr, 10))
        cap = v;
    unsigned n = jobs ? unsigned(jobs) : std::thread::hardware_concurrency();
    n = std::max(1u, std::min({n, cap, unsigned(work.size())}));
    DefaultThreadPool pool(hardware_concurrency(n));
    for (const Job &job : work)
      pool.async([&, job] { runJob(job, true); });
    pool.wait();
  }

  if (opts.timePhases) {
    double wall = std::chrono::duration<double, std::milli>(
                      std::chrono::steady_clock::now() - start).count();
    total.print(errs(), work.size() > 1 ? "sum of jobs" : work[0].in);
    errs() << format("rt-bitcode prep %.2f ms, wall %.2f ms, %zu job(s)\n", prepMs,
                     wall, work.size());
  }
  return ok ? 0 : 1;
}

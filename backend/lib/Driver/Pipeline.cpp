// Pipeline.cpp: parse, verify, MLIR passes, translate, link runtime, optimize,
// emit. Everything runs in-process. Each call owns its own MLIRContext and
// LLVMContext, so calls are safe to run in parallel.
#include "marmot/Driver/Pipeline.h"
#include "tier0/Reader.h"
#include <mutex>
#include "marmot/Driver/RuntimeBitcode.h"
#include "marmot/Driver/Targets.h"
#include "marmot/Driver/Gpu.h"
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/Dialect/LLVMIR/Transforms/Passes.h"
#include "mlir/IR/BuiltinOps.h"
#include "mlir/IR/MLIRContext.h"
#include "mlir/IR/Verifier.h"
#include "mlir/Parser/Parser.h"
#include "mlir/Pass/PassManager.h"
#include "mlir/Target/LLVMIR/Dialect/Builtin/BuiltinToLLVMIRTranslation.h"
#include "mlir/Target/LLVMIR/Dialect/LLVMIR/LLVMToLLVMIRTranslation.h"
#include "mlir/Target/LLVMIR/Export.h"

#include "llvm/ADT/StringExtras.h"
#include "llvm/Bitcode/BitcodeWriter.h"
#include "llvm/IR/LLVMContext.h"
#include "llvm/IR/LegacyPassManager.h"
#include "llvm/IR/Intrinsics.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/Verifier.h"
#include "llvm/MC/TargetRegistry.h"
#include "llvm/Passes/PassBuilder.h"
#include "llvm/Passes/StandardInstrumentations.h"
#include "llvm/Support/PGOOptions.h"
#include "llvm/Support/FileSystem.h"
#include "llvm/Support/SourceMgr.h"
#include "llvm/Support/ToolOutputFile.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Target/TargetMachine.h"
#include "llvm/Target/TargetOptions.h"
#include "llvm/TargetParser/Host.h"
#include "llvm/TargetParser/SubtargetFeature.h"
#include "mlir/Support/FileUtilities.h"
#include "llvm/Support/Format.h"

#include <chrono>
#include <thread>
#include "llvm/ADT/StringSet.h"
#include "llvm/ADT/StringMap.h"

namespace marmot {

namespace {

using Clock = std::chrono::steady_clock;

struct PhaseTimer {
  Clock::time_point start = Clock::now();
  double lap() {
    auto now = Clock::now();
    double ms = std::chrono::duration<double, std::milli>(now - start).count();
    start = now;
    return ms;
  }
};

llvm::OptimizationLevel passLevel(char l) {
  switch (l) {
  case '0': return llvm::OptimizationLevel::O0;
  case '1': return llvm::OptimizationLevel::O1;
  case '3': return llvm::OptimizationLevel::O3;
  case 's': return llvm::OptimizationLevel::Os;
  case 'z': return llvm::OptimizationLevel::Oz;
  default: return llvm::OptimizationLevel::O2;
  }
}

llvm::CodeGenOptLevel codegenLevel(char l) {
  switch (l) {
  case '0': return llvm::CodeGenOptLevel::None;
  case '1': return llvm::CodeGenOptLevel::Less;
  case '3': return llvm::CodeGenOptLevel::Aggressive;
  default: return llvm::CodeGenOptLevel::Default;
  }
}

void registerDialects(mlir::DialectRegistry &registry) {
  registry.insert<mlir::LLVM::LLVMDialect, go::GoDialect>();
  mlir::registerBuiltinDialectTranslation(registry);
  mlir::registerLLVMDialectTranslation(registry);
}

std::unique_ptr<llvm::TargetMachine>
makeTargetMachine(const CompileOptions &opts, llvm::Triple &triple,
                  std::string &err) {
  triple = targetTriple(opts.triple);
  if (!initTargetFor(triple, err))
    return nullptr;
  const llvm::Target *target = llvm::TargetRegistry::lookupTarget(triple, err);
  if (!target)
    return nullptr;
  bool isHost = false;
  std::string cpu = opts.cpu.empty() ? defaultCPU(triple, isHost) : opts.cpu;
  if (opts.cpu == "native") { // the host CPU and all its features
    cpu = llvm::sys::getHostCPUName().str();
    isHost = true;
  }
  std::string features = opts.features;
  if (isHost && (opts.cpu.empty() || opts.cpu == "native") && features.empty()) {
    llvm::SubtargetFeatures f;
    for (auto &kv : llvm::sys::getHostCPUFeatures())
      f.AddFeature(kv.first(), kv.second);
    features = f.getString();
  }
  llvm::TargetOptions to;
  to.FunctionSections = true;
  to.DataSections = true;
  // With a profile, split cold blocks of hot functions into a cold section.
  // LLVM supports the splitter for ELF only (it crashes the Mach-O asm printer).
  to.EnableMachineFunctionSplitter = !opts.pgoUse.empty() && triple.isOSBinFormatELF();
  return std::unique_ptr<llvm::TargetMachine>(target->createTargetMachine(
      triple, cpu, features, to, llvm::Reloc::PIC_, std::nullopt,
      codegenLevel(opts.optLevel)));
}

// Every function gets the target CPU and features of the target machine, as
// clang does. Without this the inliner refuses to inline runtime functions
// that were compiled with explicit target attributes.
void setTargetAttrs(llvm::Module &m, llvm::TargetMachine &tm, char level) {
  llvm::StringRef cpu = tm.getTargetCPU();
  llvm::StringRef features = tm.getTargetFeatureString();
  for (llvm::Function &f : m) {
    if (f.isDeclaration())
      continue;
    // As clang does: -Os and -Oz act through function attributes in the
    // optimizer and in code generation (including the machine outliner).
    if ((level == 's' || level == 'z') &&
        !f.hasFnAttribute(llvm::Attribute::OptimizeNone)) {
      f.addFnAttr(llvm::Attribute::OptimizeForSize);
      if (level == 'z')
        f.addFnAttr(llvm::Attribute::MinSize);
    }
    if (!cpu.empty())
      f.addFnAttr("target-cpu", cpu);
    else
      f.removeFnAttr("target-cpu");
    if (!features.empty())
      f.addFnAttr("target-features", features);
    else
      f.removeFnAttr("target-features");
  }
}

// A Go package initializer: "<pkg>.init" or "<pkg>.init#N", no parameters,
// no result. Methods named init always have a receiver parameter, and
// closures inside init are named "<pkg>.init$N", so neither matches.
bool isPackageInit(const llvm::Function &f) {
  if (f.isDeclaration() || f.arg_size() != 0 || !f.getReturnType()->isVoidTy())
    return false;
  llvm::StringRef name = f.getName();
  size_t hash = name.rfind('#');
  if (hash != llvm::StringRef::npos) {
    llvm::StringRef num = name.substr(hash + 1);
    if (num.empty() || !llvm::all_of(num, llvm::isDigit))
      return false;
    name = name.take_front(hash);
  }
  return name.ends_with(".init") && name.size() > 5;
}

// Package initializers run once. Large ones (tables built by code) dominate
// compile time at -O2 while their speed does not matter, so they are not
// optimized.
void markInitOptNone(llvm::Module &m) {
  for (llvm::Function &f : m) {
    if (!isPackageInit(f))
      continue;
    f.removeFnAttr(llvm::Attribute::AlwaysInline);
    f.removeFnAttr(llvm::Attribute::MinSize);
    f.removeFnAttr(llvm::Attribute::OptimizeForSize);
    f.addFnAttr(llvm::Attribute::OptimizeNone);
    f.addFnAttr(llvm::Attribute::NoInline);
    f.addFnAttr(llvm::Attribute::Cold);
  }
}

// Functions whose identity is observable through their frame (they call
// recover, which checks that its caller was called directly by a deferred
// call) must never become a thunk to an identical function. MergeFunctions
// compares attributes, so a unique attribute keeps each one apart.
void keepRecoverFramesUnmerged(llvm::Module &m) {
  for (llvm::Function &f : m) {
    if (f.isDeclaration())
      continue;
    bool usesFrame = false;
    for (llvm::BasicBlock &bb : f)
      for (llvm::Instruction &inst : bb)
        if (auto *call = llvm::dyn_cast<llvm::CallBase>(&inst))
          if (llvm::Function *callee = call->getCalledFunction())
            usesFrame |= callee->getName() == "runtime.gorecover" ||
                         callee->getIntrinsicID() == llvm::Intrinsic::frameaddress;
    if (usesFrame)
      f.addFnAttr("marmot-frame", f.getName());
  }
}

struct GpuLoopsPass : llvm::PassInfoMixin<GpuLoopsPass> {
  std::string arch;
  explicit GpuLoopsPass(std::string a) : arch(std::move(a)) {}
  llvm::PreservedAnalyses run(llvm::Module &m, llvm::ModuleAnalysisManager &) {
    std::string err;
    if (!lowerGpuLoops(m, arch, err))
      llvm::report_fatal_error(llvm::Twine("gpu: ") + err);
    return llvm::PreservedAnalyses::none();
  }
};

void runLLVMPipeline(llvm::Module &m, llvm::TargetMachine &tm, char level,
                     const loops::Options &lopts, const CompileOptions &opts) {
  llvm::LoopAnalysisManager lam;
  llvm::FunctionAnalysisManager fam;
  llvm::CGSCCAnalysisManager cgam;
  llvm::ModuleAnalysisManager mam;
  llvm::PipelineTuningOptions pto;
  pto.LoopUnrolling = level != 's' && level != 'z';
  pto.LoopVectorization = level == '2' || level == '3';
  pto.SLPVectorization = level == '2' || level == '3';
  // Off by default: merging two identical defer thunks or
  // wrappers adds a frame between a deferred call and recover, which then
  // returns nil (tests/run/audit/merge_recover_Os.go). -merge-functions turns
  // it on at -Os/-Oz for measurements.
  pto.MergeFunctions = opts.mergeFunctions && (level == 's' || level == 'z');
  // Standard instrumentation makes -time-passes, -print-after and friends work.
  llvm::PassInstrumentationCallbacks pic;
  llvm::StandardInstrumentations si(m.getContext(), /*DebugLogging=*/false);
  si.registerCallbacks(pic, &mam);
  // Profile guided optimization: instrument (-pgo-gen) or use (-pgo).
  std::optional<llvm::PGOOptions> pgo;
  if (!opts.pgoGen.empty() && level != '0')
    pgo = llvm::PGOOptions(opts.pgoGen, "", "", "", llvm::PGOOptions::IRInstr,
                           llvm::PGOOptions::NoCSAction, llvm::PGOOptions::ColdFuncOpt::Default,
                           /*DebugInfoForProfiling=*/false, /*PseudoProbe=*/false,
                           /*AtomicCounterUpdate=*/true); // goroutines run on many threads
  else if (!opts.pgoUse.empty() && level != '0')
    pgo = llvm::PGOOptions(opts.pgoUse, "", "", "", llvm::PGOOptions::IRUse);
  llvm::PassBuilder pb(&tm, pto, pgo, &pic);
  pb.registerModuleAnalyses(mam);
  pb.registerCGSCCAnalyses(cgam);
  pb.registerFunctionAnalyses(fam);
  pb.registerLoopAnalyses(lam);
  pb.crossRegisterProxies(lam, fam, cgam, mam);
  llvm::ModulePassManager mpm;
  llvm::OptimizationLevel ol = passLevel(level);
  if (ol == llvm::OptimizationLevel::O0) {
    mpm = pb.buildO0DefaultPipeline(ol);
  } else {
    if (lopts.enable)
      loops::registerLLVMPasses(pb, lopts);
    // GPU kernels start from the scalar outlined bodies: extract them right
    // after the loop optimizer, before CPU vectorization.
    std::string gpuArch = opts.gpuArch;
    pb.registerOptimizerEarlyEPCallback(
        [gpuArch](llvm::ModulePassManager &mpm, llvm::OptimizationLevel,
                  llvm::ThinOrFullLTOPhase) { mpm.addPass(GpuLoopsPass(gpuArch)); });
    mpm = pb.buildPerModuleDefaultPipeline(ol);
  }
  mpm.run(m, mam);
}

} // namespace

void PhaseTimes::add(const PhaseTimes &o) {
  parse += o.parse;
  mlirPasses += o.mlirPasses;
  translate += o.translate;
  link += o.link;
  linkRt += o.linkRt;
  opt += o.opt;
  codegen += o.codegen;
  write += o.write;
}

void PhaseTimes::print(llvm::raw_ostream &os, const std::string &label) const {
  os << llvm::format("%-24s parse %7.2f  mlir %7.2f  translate %7.2f  wpo %7.2f"
                     "  link-rt %7.2f  opt %7.2f  codegen %7.2f  write %6.2f  total %8.2f ms\n",
                     label.c_str(), parse, mlirPasses, translate, link, linkRt, opt,
                     codegen, write, total());
}

bool parseOptLevel(const std::string &s, char &level) {
  if (s.size() != 1 || std::string("0123sz").find(s[0]) == std::string::npos)
    return false;
  level = s[0];
  return true;
}

std::unique_ptr<llvm::TargetMachine> createTargetMachine(const CompileOptions &opts,
                                                         std::string &err) {
  llvm::Triple triple;
  return makeTargetMachine(opts, triple, err);
}

struct ParsedModule {
  std::unique_ptr<mlir::MLIRContext> ctx;
  llvm::SourceMgr sm;
  std::unique_ptr<mlir::SourceMgrDiagnosticHandler> diag;
  std::string diagText;
  std::unique_ptr<llvm::raw_string_ostream> diagStream;
  mlir::OwningOpRef<mlir::ModuleOp> module;
  bool verified = false;  // mlir::verify passed (see parseFile)
};

void ParsedModuleDeleter::operator()(ParsedModule *p) const { delete p; }

void *parsedModuleOp(ParsedModule &pm) { return pm.module->getOperation(); }

namespace {
// A large module parses in parallel (MLIR's parser is serial: a 16 MB module
// took 0.4 s, the longest job of a deep edit). The body is cut at top-level
// op boundaries into chunks; each is parsed as a module of its own in the
// same context, with the text before the body (type aliases, the module
// line) and the location aliases it uses, and the ops are moved into one
// module, verified once. Text not in the shape the printer writes, or any
// error, falls back to the serial parse (which gives the diagnostics).
// The shared context's locks cost: 4 threads parse that module in 0.22 s
// for 1.4x the CPU time of the serial parse, more threads gain nothing; so
// only threads the batch has spare (CompileOptions::parseThreads).
constexpr size_t kParallelParseMin = 2u << 20;
constexpr size_t kParseChunk = 1u << 20;

struct ModuleSplit {
  std::string head;                         // before the "module ... {" line, but aliases
  llvm::StringRef moduleLine;               // that line
  std::vector<llvm::StringRef> chunks;      // the body, cut between ops
  llvm::StringMap<llvm::StringRef> aliases; // attribute aliases: name -> line
};

// An attribute alias line ("#loc3 = loc(...)") goes in aliases.
bool addAlias(llvm::StringRef line, ModuleSplit &out) {
  size_t eq = line.find(" = ");
  if (!line.starts_with("#") || eq == llvm::StringRef::npos)
    return false;
  out.aliases[line.take_front(eq)] = line;
  return true;
}

bool aliasChar(char c) { return llvm::isAlnum(c) || c == '_' || c == '$' || c == '.' || c == '-'; }

bool splitModule(llvm::StringRef text, unsigned n, ModuleSplit &out) {
  size_t pos = 0;
  for (;;) {
    size_t eol = text.find('\n', pos);
    if (eol == llvm::StringRef::npos)
      return false;
    llvm::StringRef line = text.slice(pos, eol);
    pos = eol + 1;
    if (line.starts_with("module")) {
      if (!line.rtrim().ends_with("{"))
        return false;
      out.moduleLine = text.slice(eol - line.size(), pos);
      break;
    }
    // Location aliases before the module (where the frontend writes them,
    // so no op refers to one ahead of its definition): each chunk takes
    // the ones it uses.
    if (!addAlias(line, out))
      out.head.append(text.data() + (eol - line.size()), line.size() + 1);
  }
  size_t per = (text.size() - pos) / n + 1, start = pos, target = pos + per;
  size_t end = llvm::StringRef::npos;
  int depth = 0;
  for (size_t i = pos; i < text.size() && end == llvm::StringRef::npos; ++i) {
    char c = text[i];
    switch (c) {
    case '"':
      for (++i; i < text.size() && text[i] != '"'; ++i)
        if (text[i] == '\\')
          ++i;
      break;
    case '/':
      if (i + 1 < text.size() && text[i + 1] == '/') {
        i = text.find('\n', i);
        if (i == llvm::StringRef::npos)
          return false;
        --i; // the newline is a boundary
      }
      break;
    case '{':
      ++depth;
      break;
    case '}':
      if (depth == 0)
        end = i;
      else
        --depth;
      break;
    case '\n':
      if (depth == 0 && i + 1 >= target) {
        out.chunks.push_back(text.slice(start, i + 1));
        start = i + 1;
        target = start + per;
      }
      break;
    }
  }
  if (end == llvm::StringRef::npos || (end > 0 && text[end - 1] != '\n'))
    return false;
  if (start < end)
    out.chunks.push_back(text.slice(start, end));
  llvm::StringRef rest = text.substr(end + 1);
  while (!rest.empty()) {
    auto [line, next] = rest.split('\n');
    rest = next;
    if (line.trim().empty())
      continue;
    if (!addAlias(line, out))
      return false;
  }
  return true;
}

// The text of chunk i as a module of its own.
std::string chunkText(const ModuleSplit &sp, size_t i) {
  llvm::StringRef chunk = sp.chunks[i];
  std::vector<llvm::StringRef> used, work{chunk};
  llvm::StringSet<> seen;
  while (!work.empty()) {
    llvm::StringRef t = work.back();
    work.pop_back();
    for (size_t h = t.find('#'); h != llvm::StringRef::npos; h = t.find('#', h + 1)) {
      size_t j = h + 1;
      while (j < t.size() && aliasChar(t[j]))
        ++j;
      auto it = sp.aliases.find(t.slice(h, j));
      if (it != sp.aliases.end() && seen.insert(it->first()).second) {
        used.push_back(it->second);
        work.push_back(it->second.substr(it->first().size()));
      }
    }
  }
  // In the order of the file.
  llvm::sort(used, [](llvm::StringRef a, llvm::StringRef b) { return a.data() < b.data(); });
  // The aliases before the module line: defined before their uses.
  size_t size = sp.head.size() + sp.moduleLine.size() + chunk.size() + 2;
  for (llvm::StringRef u : used)
    size += u.size() + 1;
  std::string s;
  s.reserve(size);
  s += sp.head;
  for (llvm::StringRef u : used) {
    s.append(u.data(), u.size());
    s += '\n';
  }
  s.append(sp.moduleLine.data(), sp.moduleLine.size());
  s.append(chunk.data(), chunk.size());
  s += "}\n";
  return s;
}

// Constant globals (type descriptors, itabs, closures) come from the
// frontend without an initializer region: a "go.init" string holds the
// value tree, and the region is built here. An initializer as text is about
// 36 ops for one type descriptor, and parsing an op (SSA names, a type, a
// location, the op itself) cost a third of the parse of a cold build.
// Grammar, types taken from the enclosing aggregate (the global's type at
// the top):
//   value := 'i' DEC ';'          integer of the element type
//          | 'f' HEX16             float: the bits of a double
//          | 'z'                   zero
//          | 'a' DEC ':' NAME      address of the symbol NAME of DEC bytes
//          | '(' value* ')'        aggregate, one value per element
// The ops are the ones the text form had, in the same order: the elements
// first, then undef and one insertvalue per element; but an array of
// integers is one constant with a dense value (intArray).
constexpr llvm::StringLiteral kGoInit = "go.init";

struct InitBuilder {
  mlir::OpBuilder &b;
  mlir::Location loc;
  llvm::StringRef s;
  size_t pos = 0;
  mlir::Type ptr;

  bool fail() { return false; }
  mlir::Value intArray(mlir::LLVM::LLVMArrayType at);
  bool number(int64_t &v) {
    bool neg = pos < s.size() && s[pos] == '-';
    if (neg)
      ++pos;
    size_t start = pos;
    uint64_t u = 0;
    while (pos < s.size() && llvm::isDigit(s[pos]))
      u = u * 10 + uint64_t(s[pos++] - '0');
    v = neg ? int64_t(-u) : int64_t(u);
    return pos > start;
  }
  mlir::Value value(mlir::Type ty) {
    if (pos >= s.size())
      return {};
    char c = s[pos++];
    switch (c) {
    case 'i': {
      int64_t v;
      if (!number(v) || pos >= s.size() || s[pos++] != ';')
        return {};
      auto it = llvm::dyn_cast<mlir::IntegerType>(ty);
      if (!it)
        return {};
      llvm::APInt ap(64, uint64_t(v));
      return b.create<mlir::LLVM::ConstantOp>(
          loc, ty, b.getIntegerAttr(ty, ap.trunc(it.getWidth())));
    }
    case 'f': {
      if (pos + 16 > s.size())
        return {};
      uint64_t bits;
      if (s.substr(pos, 16).getAsInteger(16, bits))
        return {};
      pos += 16;
      double d = llvm::bit_cast<double>(bits);
      mlir::FloatAttr a;
      if (ty.isF64())
        a = b.getF64FloatAttr(d);
      else if (ty.isF32())
        a = b.getF32FloatAttr(float(d));
      else
        return {};
      return b.create<mlir::LLVM::ConstantOp>(loc, ty, a);
    }
    case 'z':
      return b.create<mlir::LLVM::ZeroOp>(loc, ty);
    case 'a': {
      int64_t n;
      if (!number(n) || n < 0 || pos >= s.size() || s[pos++] != ':' || pos + n > s.size())
        return {};
      llvm::StringRef name = s.substr(pos, n);
      pos += n;
      return b.create<mlir::LLVM::AddressOfOp>(loc, ptr, name);
    }
    case '(': {
      llvm::SmallVector<mlir::Value, 8> elems;
      auto st = llvm::dyn_cast<mlir::LLVM::LLVMStructType>(ty);
      auto at = llvm::dyn_cast<mlir::LLVM::LLVMArrayType>(ty);
      if (!st && !at)
        return {};
      if (at)
        if (mlir::Value v = intArray(at))
          return v;
      while (pos < s.size() && s[pos] != ')') {
        unsigned i = elems.size();
        mlir::Type et;
        if (st) {
          if (i >= st.getBody().size())
            return {};
          et = st.getBody()[i];
        } else {
          if (i >= at.getNumElements())
            return {};
          et = at.getElementType();
        }
        mlir::Value v = value(et);
        if (!v)
          return {};
        elems.push_back(v);
      }
      if (pos >= s.size())
        return {};
      ++pos;
      mlir::Value agg = b.create<mlir::LLVM::UndefOp>(loc, ty);
      for (auto [i, v] : llvm::enumerate(elems))
        agg = b.create<mlir::LLVM::InsertValueOp>(loc, agg, v, llvm::ArrayRef<int64_t>{int64_t(i)});
      return agg;
    }
    }
    return {};
  }
};

// An array of integers (a table, embedded bytes) as one constant with a
// dense value: LLVM turns an insertvalue chain into a new constant array per
// element, quadratic in the length (an 88 KB embedded table in
// crypto/internal/fips140/nistec took 89 s to translate). Null, with pos
// where it was, unless every element is an integer or zero.
mlir::Value InitBuilder::intArray(mlir::LLVM::LLVMArrayType at) {
  auto it = llvm::dyn_cast<mlir::IntegerType>(at.getElementType());
  if (!it || at.getNumElements() < 2)
    return {};
  size_t start = pos;
  llvm::SmallVector<llvm::APInt> vals;
  vals.reserve(at.getNumElements());
  while (pos < s.size() && s[pos] != ')') {
    int64_t v = 0;
    if (s[pos] == 'z') {
      ++pos;
    } else if (s[pos] == 'i') {
      ++pos;
      if (!number(v) || pos >= s.size() || s[pos++] != ';') {
        pos = start;
        return {};
      }
    } else {
      pos = start;
      return {};
    }
    vals.push_back(llvm::APInt(64, uint64_t(v)).trunc(it.getWidth()));
  }
  if (pos >= s.size() || vals.size() != at.getNumElements()) {
    pos = start;
    return {};
  }
  ++pos;
  auto tt = mlir::RankedTensorType::get({int64_t(vals.size())}, it);
  return b.create<mlir::LLVM::ConstantOp>(loc, at, mlir::DenseElementsAttr::get(tt, vals));
}

// Builds the initializer region of each global with a go.init string.
bool expandInits(mlir::ModuleOp m, llvm::raw_ostream &errs) {
  mlir::OpBuilder b(m.getContext());
  mlir::Type ptr = mlir::LLVM::LLVMPointerType::get(m.getContext());
  for (auto g : m.getOps<mlir::LLVM::GlobalOp>()) {
    auto init = g->getAttrOfType<mlir::StringAttr>(kGoInit);
    if (!init)
      continue;
    g->removeAttr(kGoInit);
    mlir::Block *blk = new mlir::Block;
    g.getInitializerRegion().push_back(blk);
    b.setInsertionPointToEnd(blk);
    InitBuilder ib{b, g.getLoc(), init.getValue(), 0, ptr};
    mlir::Value v = ib.value(g.getGlobalType());
    if (!v || ib.pos != ib.s.size()) {
      errs << "global " << g.getSymName() << ": bad " << kGoInit << " at offset " << ib.pos << "\n";
      return false;
    }
    b.create<mlir::LLVM::ReturnOp>(g.getLoc(), v);
  }
  return true;
}

// Parses text into one module in parallel; null if it cannot (the caller
// parses serially).
mlir::OwningOpRef<mlir::ModuleOp> parseParallel(mlir::MLIRContext *ctx, llvm::StringRef text,
                                                llvm::StringRef name, unsigned threads, bool verify) {
  unsigned n = std::min<size_t>(threads, text.size() / kParseChunk);
  ModuleSplit sp;
  if (n < 2 || !splitModule(text, n, sp) || sp.chunks.size() < 2)
    return nullptr;
  // The parser loads dialects on first use, which is not thread safe.
  ctx->loadAllAvailableDialects();
  bool threaded = ctx->isMultithreadingEnabled();
  if (!threaded)
    ctx->enableMultithreading();
  std::vector<mlir::OwningOpRef<mlir::ModuleOp>> mods(sp.chunks.size());
  {
    std::vector<std::thread> ts;
    for (size_t i = 0; i < sp.chunks.size(); ++i)
      ts.emplace_back([&, i] {
        std::string t = chunkText(sp, i);
        llvm::SourceMgr sm;
        sm.AddNewSourceBuffer(llvm::MemoryBuffer::getMemBuffer(t, name, false), llvm::SMLoc());
        mods[i] = mlir::parseSourceFile<mlir::ModuleOp>(
            sm, mlir::ParserConfig(ctx, /*verifyAfterParse=*/false));
        // A bad go.init fails here too; the serial parse reports it.
        if (mods[i] && !expandInits(*mods[i], llvm::nulls()))
          mods[i] = nullptr;
      });
    for (std::thread &t : ts)
      t.join();
  }
  bool ok = llvm::all_of(mods, [](auto &m) { return bool(m); });
  if (ok) {
    mlir::Block *dst = mods[0]->getBody();
    for (size_t i = 1; i < mods.size(); ++i)
      dst->getOperations().splice(dst->end(), mods[i]->getBody()->getOperations());
    mods.resize(1);
    ok = !verify || mlir::succeeded(mlir::verify(*mods[0]));
  }
  if (!threaded)
    ctx->disableMultithreading();
  return ok ? std::move(mods[0]) : nullptr;
}
} // namespace

// Verification is skipped for tier0 batches (dev builds): it is about a
// fifth of parsing. MARMOT_VERIFY=1 keeps it. A module that goes to LLVM
// in whole or in part is verified first (verifyParsed): tier0 compiles only
// the operations, types and attributes it knows and leaves anything else to
// that path, so an invalid module is reported there, not compiled.
static bool forceVerify() {
  static const bool on = [] {
    const char *v = getenv("MARMOT_VERIFY");
    return v && *v && *v != '0';
  }();
  return on;
}

bool verifyParsed(ParsedModule &pm, const std::string &input, llvm::raw_ostream &errs) {
  if (pm.verified)
    return true;
  if (mlir::failed(mlir::verify(*pm.module))) {
    errs << pm.diagText << input << ": invalid module (internal compiler error)\n";
    pm.diagText.clear();
    return false;
  }
  pm.verified = true;
  return true;
}

static bool envOn(const char *name) {
  const char *v = getenv(name);
  return v && *v && *v != '0';
}
// On unless MARMOT_FAST_PARSE=0.
static bool fastParse() {
  static const bool on = [] {
    const char *v = getenv("MARMOT_FAST_PARSE");
    return !v || !*v || *v != '0';
  }();
  return on;
}
static bool parseCheck() {
  static const bool on = envOn("MARMOT_PARSE_CHECK");
  return on;
}

// The two modules print the same in generic form with locations; else the
// first differing line of each in diff.
static bool sameModule(mlir::ModuleOp a, mlir::ModuleOp b, std::string &diff) {
  auto print = [](mlir::ModuleOp m) {
    std::string s;
    llvm::raw_string_ostream os(s);
    m->print(os, mlir::OpPrintingFlags().printGenericOpForm().enableDebugInfo().assumeVerified());
    return s;
  };
  std::string pa = print(a), pb = print(b);
  if (pa == pb)
    return true;
  size_t k = 0;
  while (k < pa.size() && k < pb.size() && pa[k] == pb[k])
    ++k;
  auto lineAt = [&](const std::string &s) {
    size_t ls = s.rfind('\n', k);
    ls = ls == std::string::npos ? 0 : ls + 1;
    return s.substr(ls, std::min<size_t>(s.find('\n', k) - ls, 400));
  };
  diff = "  parser: " + lineAt(pa) + "\n  reader: " + lineAt(pb) + "\n";
  return false;
}

ParsedModulePtr parseFile(const std::string &input, const CompileOptions &opts,
                          llvm::raw_ostream &errs) {
  ParsedModulePtr pm(new ParsedModule);
  const bool verify = !opts.tier0 || forceVerify();
  mlir::DialectRegistry registry;
  registerDialects(registry);
  pm->ctx = std::make_unique<mlir::MLIRContext>(
      registry, opts.singleThreaded ? mlir::MLIRContext::Threading::DISABLED
                                    : mlir::MLIRContext::Threading::ENABLED);
  std::string ioErr;
  auto inBuf = mlir::openInputFile(input, &ioErr);
  if (!inBuf) {
    errs << input << ": " << ioErr << "\n";
    return nullptr;
  }
  pm->sm.AddNewSourceBuffer(std::move(inBuf), llvm::SMLoc());
  // Diagnostics are collected and printed by whoever reports this job.
  pm->diagStream = std::make_unique<llvm::raw_string_ostream>(pm->diagText);
  pm->diag = std::make_unique<mlir::SourceMgrDiagnosticHandler>(pm->sm, pm->ctx.get(),
                                                                 *pm->diagStream);
  llvm::StringRef text = pm->sm.getMemoryBuffer(1)->getBuffer();
  // tier0 batches read the frontend's text with the tier0 reader
  // (MARMOT_FAST_PARSE=0 turns it off), which gives up on text it does not
  // know; the MLIR parser reads that. MARMOT_PARSE_CHECK=1 reads
  // every module both ways and fails when the modules differ.
  bool check = parseCheck();
  if (opts.tier0 && (fastParse() || check)) {
    std::string why;
    pm->module = tier0::readModule(pm->ctx.get(), text, input, why);
    if (check && pm->module) {
      mlir::OwningOpRef<mlir::ModuleOp> ref = mlir::parseSourceFile<mlir::ModuleOp>(
          pm->sm, mlir::ParserConfig(pm->ctx.get(), /*verifyAfterParse=*/false));
      std::string diff;
      if (!ref) {
        errs << pm->diagText << input << ": parse check: the reader read a module the parser rejects\n";
        return nullptr;
      }
      if (!sameModule(*ref, *pm->module, diff)) {
        errs << input << ": parse check: the reader and the parser differ\n" << diff;
        return nullptr;
      }
    }
    if (pm->module && !fastParse())
      pm->module = nullptr;  // check only: use the parser's module as before
    if (pm->module && (!expandInits(*pm->module, *pm->diagStream) ||
                       (verify && mlir::failed(mlir::verify(*pm->module)))))
      pm->module = nullptr;
  }
  // Only tier0 batches size parseThreads to the threads other jobs leave.
  if (!pm->module && opts.tier0 && opts.parseThreads > 1 && text.size() >= kParallelParseMin) {
    pm->module = parseParallel(pm->ctx.get(), text, input, opts.parseThreads, verify);
    if (!pm->module)
      pm->diagText.clear();
  }
  // Verified once, after the initializers are built.
  if (!pm->module) {
    pm->module = mlir::parseSourceFile<mlir::ModuleOp>(
        pm->sm, mlir::ParserConfig(pm->ctx.get(), /*verifyAfterParse=*/false));
    if (pm->module && (!expandInits(*pm->module, *pm->diagStream) ||
                       (verify && mlir::failed(mlir::verify(*pm->module)))))
      pm->module = nullptr;
  }
  if (!pm->module) {
    errs << pm->diagText << input << ": failed to parse\n";
    return nullptr;
  }
  pm->verified = verify;
  // Ops without a frontend location (wrappers, synthetic code) get the
  // parser's location in the input .mlir file: a temporary path and a line
  // that moves with every edit of the module. They get "<autogenerated>":1
  // instead, as gc names wrapper code (tracebacks showed m002.mlir:0, and
  // cache keys changed with every edit).
  if (opts.debugLevel > 0) {
    auto file = mlir::StringAttr::get(pm->ctx.get(), input);
    mlir::Location unknown = mlir::FileLineColLoc::get(pm->ctx.get(), "<autogenerated>", 1, 0);
    // Only in modules with frontend locations (others get no debug info).
    bool frontendLocs = false;
    pm->module->walk([&](mlir::Operation *op) {
      if (auto fl = llvm::dyn_cast<mlir::FileLineColLoc>(op->getLoc());
          fl && fl.getFilename() != file) {
        frontendLocs = true;
        return mlir::WalkResult::interrupt();
      }
      return mlir::WalkResult::advance();
    });
    auto self = [&](mlir::Location l) {
      auto fl = llvm::dyn_cast<mlir::FileLineColLoc>(l);
      return fl && fl.getFilename() == file;
    };
    // Block arguments (phis) need no location.
    mlir::Location none = mlir::UnknownLoc::get(pm->ctx.get());
    // The module's own location names the DWARF compile unit: it needs a
    // directory (ld writes no debug map for a unit without one, and dsymutil
    // then finds no DWARF), and must not be the temporary path.
    if (frontendLocs)
      pm->module->getOperation()->setLoc(
          mlir::FileLineColLoc::get(pm->ctx.get(), "/marmot/module.mlir", 1, 0));
    if (frontendLocs)
      pm->module->walk([&](mlir::Operation *op) {
        if (op == pm->module->getOperation())
          return;
        if (self(op->getLoc()))
          op->setLoc(unknown);
        for (mlir::Region &r : op->getRegions())
          for (mlir::Block &b : r)
            for (mlir::BlockArgument a : b.getArguments())
              if (self(a.getLoc()))
                a.setLoc(none);
      });
  }
  return pm;
}

namespace {
// The go passes and the lowering to the llvm dialect.
bool runMLIRPasses(ParsedModule &pm, const std::string &input, const CompileOptions &opts,
                   PhaseTimes &times, llvm::raw_ostream &errs) {
  PhaseTimer timer;
  auto fail = [&](const llvm::Twine &msg) {
    errs << pm.diagText;
    pm.diagText.clear();
    errs << input << ": " << msg << "\n";
    return false;
  };
  mlir::ModuleOp module = *pm.module;

  // MLIR passes: go dialect passes and lowering to the llvm dialect.
  {
    mlir::PassManager mpm(pm.ctx.get(), mlir::ModuleOp::getOperationName());
    // -O0 verifies after every pass (a debugging level), except for tier0
    // (build time; the parser verified the input).
    mpm.enableVerifier(opts.verifyEach || (opts.optLevel == '0' && !opts.tier0));
    if (opts.optLevel != '0') {
      if (opts.strings)
        mpm.addPass(go::createGoStrings(go::GoStringsOptions{opts.stringsNoCopy}));
      if (opts.devirt)
        mpm.addPass(go::createGoDevirt());
      if (opts.boolPack)
        mpm.addPass(go::createGoBoolPack());
      if (opts.escape)
        mpm.addPass(go::createGoEscape());
      if (opts.bce)
        mpm.addPass(go::createGoBCE());
      if (opts.openDefers)
        mpm.addPass(go::createGoOpenDefers());
    }
    mpm.addPass(go::createGoToLLVM());
    // Always: large aggregate values would hang code generation.
    mpm.addPass(go::createAggregateCopies());
    // Line tables (or full debug info) from the op locations the frontend
    // emits: a DISubprogram per function, a DILocation per instruction.
    bool hasFileLocs =
        opts.debugLevel > 0 &&
        module
            .walk([](mlir::Operation *op) {
              // Locations into the .mlir text itself come from the parser,
              // not from the frontend; they do not count.
              auto fl = mlir::dyn_cast<mlir::FileLineColLoc>(op->getLoc());
              if (fl && !fl.getFilename().getValue().ends_with(".mlir") &&
                  !fl.getFilename().getValue().ends_with(".mlirbc"))
                return mlir::WalkResult::interrupt();
              return mlir::WalkResult::advance();
            })
            .wasInterrupted();
    if (hasFileLocs) {
      mlir::LLVM::DIScopeForLLVMFuncOpPassOptions dopts;
      dopts.emissionKind = opts.debugLevel > 1 ? mlir::LLVM::DIEmissionKind::Full
                                               : mlir::LLVM::DIEmissionKind::LineTablesOnly;
      mpm.addPass(mlir::LLVM::createDIScopeForLLVMFuncOpPass(dopts));
    }
    if ((opts.optLevel == '2' || opts.optLevel == '3') && opts.loops.enable)
      loops::addMLIRPasses(mpm, opts.loops);
    if (mlir::failed(mpm.run(module)))
      return fail("MLIR pass pipeline failed");
  }
  times.mlirPasses += timer.lap();
  return true;
}

// The target data layout must be known during translation: LLVM folds a
// struct GEP on a global into a byte offset as soon as it builds it, with the
// module's data layout. Set after translation, the default layout (i64
// aligned to 4) placed fields 4 bytes off from the target's layout of the
// initializer (test driver/global-layout.mlir).
void setTargetAttrs(mlir::ModuleOp module, llvm::TargetMachine &tm) {
  module->setAttr(mlir::LLVM::LLVMDialect::getDataLayoutAttrName(),
                  mlir::StringAttr::get(module->getContext(),
                                        tm.createDataLayout().getStringRepresentation()));
  module->setAttr(mlir::LLVM::LLVMDialect::getTargetTripleAttrName(),
                  mlir::StringAttr::get(module->getContext(), tm.getTargetTriple().str()));
}
} // namespace

bool lowerModule(ParsedModule &pm, const std::string &input, const CompileOptions &opts,
                 llvm::TargetMachine &tm, PhaseTimes &times, llvm::raw_ostream &errs) {
  if (!runMLIRPasses(pm, input, opts, times, errs))
    return false;
  setTargetAttrs(*pm.module, tm);
  return true;
}

// Each type link entry (a pointer in an ELF marmot_tlinks section) is
// associated with the descriptor it points to: SHF_LINK_ORDER, so the
// linker keeps the entry only while that descriptor is live. Mach-O gets the
// same from its live_support section.
static void associateTypeLinks(llvm::Module &m) {
  if (!llvm::Triple(m.getTargetTriple()).isOSBinFormatELF()) return;
  for (llvm::GlobalVariable &g : m.globals()) {
    if (g.getSection() != "marmot_tlinks" || !g.hasInitializer()) continue;
    auto *t = llvm::dyn_cast<llvm::GlobalObject>(g.getInitializer()->stripPointerCasts());
    if (!t || t->isDeclaration()) continue;
    g.setMetadata(llvm::LLVMContext::MD_associated,
                  llvm::MDNode::get(m.getContext(), llvm::ValueAsMetadata::get(t)));
  }
}

std::unique_ptr<llvm::Module> translateLowered(ParsedModule &pm, const std::string &input,
                                               llvm::TargetMachine &tm, llvm::LLVMContext &lctx,
                                               PhaseTimes &times, llvm::raw_ostream &errs) {
  PhaseTimer timer;
  // LLVM never sees a module that was not verified.
  if (!verifyParsed(pm, input, errs))
    return nullptr;
  std::unique_ptr<llvm::Module> lmod = mlir::translateModuleToLLVMIR(*pm.module, lctx, input);
  if (!lmod) {
    errs << pm.diagText;
    pm.diagText.clear();
    errs << input << ": failed to translate to LLVM IR\n";
    return nullptr;
  }
  lmod->setTargetTriple(tm.getTargetTriple());
  lmod->setDataLayout(tm.createDataLayout());
  associateTypeLinks(*lmod);
  pm.module = nullptr; // free MLIR memory before the LLVM pipeline
  times.translate += timer.lap();
  return lmod;
}

std::unique_ptr<llvm::Module> lowerAndTranslate(ParsedModule &pm, const std::string &input,
                                                const CompileOptions &opts,
                                                llvm::TargetMachine &tm,
                                                llvm::LLVMContext &lctx, PhaseTimes &times,
                                                llvm::raw_ostream &errs,
                                                const std::string *mlirOutput) {
  if (!runMLIRPasses(pm, input, opts, times, errs))
    return nullptr;
  PhaseTimer timer;
  auto fail = [&](const llvm::Twine &msg) -> std::unique_ptr<llvm::Module> {
    errs << input << ": " << msg << "\n";
    return nullptr;
  };
  mlir::ModuleOp module = *pm.module;
  if (mlirOutput) {
    std::error_code ec;
    llvm::ToolOutputFile out(*mlirOutput, ec, llvm::sys::fs::OF_None);
    if (ec)
      return fail(*mlirOutput + ": " + ec.message());
    module.print(out.os());
    out.keep();
    times.write += timer.lap();
    return nullptr;
  }

  setTargetAttrs(module, tm);
  return translateLowered(pm, input, tm, lctx, times, errs);
}

std::unique_ptr<llvm::Module> translateFile(const std::string &input,
                                            const CompileOptions &opts,
                                            llvm::TargetMachine &tm,
                                            llvm::LLVMContext &lctx, PhaseTimes &times,
                                            llvm::raw_ostream &errs,
                                            const std::string *mlirOutput) {
  PhaseTimer timer;
  ParsedModulePtr pm = parseFile(input, opts, errs);
  if (!pm)
    return nullptr;
  times.parse += timer.lap();
  return lowerAndTranslate(*pm, input, opts, tm, lctx, times, errs, mlirOutput);
}

bool optimizeAndEmit(llvm::Module &lmod, llvm::TargetMachine &tm, const std::string &input,
                     const std::string &output, const CompileOptions &opts,
                     PhaseTimes &times, llvm::raw_ostream &errs) {
  PhaseTimer timer;
  auto fail = [&](const llvm::Twine &msg) {
    errs << input << ": " << msg << "\n";
    return false;
  };
  std::string err;
  // Runtime bitcode is only useful when the optimizer can inline it.
  if (opts.rtBitcode && opts.optLevel != '0') {
    const RuntimeLib *rt = opts.rtBitcode->get(err);
    if (!rt || !linkRuntimeBitcode(lmod, *rt, err))
      return fail(err);
  }
  setTargetAttrs(lmod, tm, opts.optLevel);
  keepRecoverFramesUnmerged(lmod);
  // -keep-call-sites (dev builds and -release -trace=full, where the pc table
  // has inlined frames): calls in Go code are nomerge, so neither SimplifyCFG
  // (sinking, hoisting) nor code generation (tail merging) folds two calls to
  // the same function from different call sites into one. A merged call keeps
  // only the common scope of both locations, so runtime.Caller lost an
  // inlined frame (log TestAll: Println and Printf, inlined into testPrint,
  // both call output). gc never merges call sites. Set before the optimizer:
  // calls the inliner copies keep the attribute.
  if (opts.keepCallSites)
    for (llvm::Function &f : lmod)
      if (!f.isDeclaration() && f.getSubprogram())
        for (llvm::BasicBlock &bb : f)
          for (llvm::Instruction &inst : bb)
            if (auto *cb = llvm::dyn_cast<llvm::CallBase>(&inst); cb && !cb->isInlineAsm() &&
                !llvm::isa<llvm::IntrinsicInst>(cb))
              cb->addFnAttr(llvm::Attribute::NoMerge);
  if (opts.initOptNone && opts.optLevel != '0')
    markInitOptNone(lmod);
  times.linkRt += timer.lap();

  if (opts.verifyEach && llvm::verifyModule(lmod, &errs))
    return fail("LLVM IR verification failed");
  runLLVMPipeline(lmod, tm, opts.optLevel, opts.loops, opts);
  // With line info (the pc table, tracebacks), Go functions keep their frames
  // as gc does: code generation emits no sibling calls, so a caller that ends
  // with a call still shows up in a traceback. Set after the optimizer, so
  // TailCallElim still turns self recursion into loops (bintrees check():
  // 10% more instructions without it). Runtime functions keep sibling calls
  // (tracebacks hide them).
  if (opts.debugLevel > 0 && !opts.keepTailCalls)
    for (llvm::Function &f : lmod)
      if (!f.isDeclaration() && f.getSubprogram() && !f.getName().starts_with("runtime.") &&
          !f.getName().starts_with("internal/"))
        f.addFnAttr("disable-tail-calls", "true");
  // At -O0 (no pipeline) marmot_gpu_for calls may still be there: CPU path.
  if (!lowerGpuLoops(lmod, "", err))
    return fail(err);
  times.opt += timer.lap();

  std::error_code ec;
  bool binary = opts.emit == Emit::Obj || opts.emit == Emit::BC;
  llvm::ToolOutputFile out(output, ec,
                           binary ? llvm::sys::fs::OF_None : llvm::sys::fs::OF_Text);
  if (ec)
    return fail(output + ": " + ec.message());
  switch (opts.emit) {
  case Emit::LLVM:
    lmod.print(out.os(), nullptr);
    break;
  case Emit::BC:
    llvm::WriteBitcodeToFile(lmod, out.os());
    break;
  case Emit::Obj:
  case Emit::Asm: {
    llvm::SmallVector<char, 0> buf;
    {
      llvm::raw_svector_ostream bos(buf);
      llvm::legacy::PassManager cg;
      auto ft = opts.emit == Emit::Obj ? llvm::CodeGenFileType::ObjectFile
                                       : llvm::CodeGenFileType::AssemblyFile;
      if (tm.addPassesToEmitFile(cg, bos, nullptr, ft))
        return fail("target cannot emit this file type");
      cg.run(lmod);
    }
    times.codegen += timer.lap();
    out.os().write(buf.data(), buf.size());
    break;
  }
  case Emit::MLIR:
    break;
  }
  out.keep();
  times.write += timer.lap();
  return true;
}

bool compileFile(const std::string &input, const std::string &output,
                 const CompileOptions &opts, PhaseTimes &times,
                 llvm::raw_ostream &errs) {
  std::string err;
  std::unique_ptr<llvm::TargetMachine> tm = createTargetMachine(opts, err);
  if (!tm) {
    errs << input << ": " << err << "\n";
    return false;
  }
  llvm::LLVMContext lctx;
  if (opts.emit == Emit::MLIR) {
    // Prints the MLIR after the go passes; a null result with no error.
    std::string msgs;
    llvm::raw_string_ostream es(msgs);
    translateFile(input, opts, *tm, lctx, times, es, &output);
    errs << msgs;
    return msgs.empty();
  }
  std::unique_ptr<llvm::Module> lmod = translateFile(input, opts, *tm, lctx, times, errs);
  if (!lmod)
    return false;
  return optimizeAndEmit(*lmod, *tm, input, output, opts, times, errs);
}

} // namespace marmot

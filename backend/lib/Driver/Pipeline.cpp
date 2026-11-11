// Pipeline.cpp: parse, verify, MLIR passes, translate, link runtime, optimize,
// emit. Everything runs in-process. Each call owns its own MLIRContext and
// LLVMContext, so calls are safe to run in parallel.
#include "marmot/Driver/Pipeline.h"
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
  std::string features = opts.features;
  if (isHost && opts.cpu.empty() && features.empty()) {
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
  // Go generic instances and wrappers often compile to identical code.
  pto.MergeFunctions = level == 's' || level == 'z';
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
    // after the loop optimizer, before CPU vectorization (docs/gpu.md).
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
};

void ParsedModuleDeleter::operator()(ParsedModule *p) const { delete p; }

void *parsedModuleOp(ParsedModule &pm) { return pm.module->getOperation(); }

ParsedModulePtr parseFile(const std::string &input, const CompileOptions &opts,
                          llvm::raw_ostream &errs) {
  ParsedModulePtr pm(new ParsedModule);
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
  // The parser verifies the module once.
  pm->module = mlir::parseSourceFile<mlir::ModuleOp>(pm->sm, mlir::ParserConfig(pm->ctx.get()));
  if (!pm->module) {
    errs << pm->diagText << input << ": failed to parse\n";
    return nullptr;
  }
  return pm;
}

std::unique_ptr<llvm::Module> lowerAndTranslate(ParsedModule &pm, const std::string &input,
                                                const CompileOptions &opts,
                                                llvm::TargetMachine &tm,
                                                llvm::LLVMContext &lctx, PhaseTimes &times,
                                                llvm::raw_ostream &errs,
                                                const std::string *mlirOutput) {
  PhaseTimer timer;
  auto fail = [&](const llvm::Twine &msg) -> std::unique_ptr<llvm::Module> {
    errs << pm.diagText;
    pm.diagText.clear();
    errs << input << ": " << msg << "\n";
    return nullptr;
  };
  mlir::ModuleOp module = *pm.module;

  // MLIR passes: go dialect passes and lowering to the llvm dialect.
  {
    mlir::PassManager mpm(pm.ctx.get(), mlir::ModuleOp::getOperationName());
    mpm.enableVerifier(opts.verifyEach || opts.optLevel == '0');
    if (opts.optLevel != '0') {
      if (opts.strings)
        mpm.addPass(go::createGoStrings());
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

  std::unique_ptr<llvm::Module> lmod = mlir::translateModuleToLLVMIR(module, lctx, input);
  if (!lmod)
    return fail("failed to translate to LLVM IR");
  lmod->setTargetTriple(tm.getTargetTriple());
  lmod->setDataLayout(tm.createDataLayout());
  pm.module = nullptr; // free MLIR memory before the LLVM pipeline
  times.translate += timer.lap();
  return lmod;
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
  if (opts.initOptNone && opts.optLevel != '0')
    markInitOptNone(lmod);
  times.linkRt += timer.lap();

  if (opts.verifyEach && llvm::verifyModule(lmod, &errs))
    return fail("LLVM IR verification failed");
  runLLVMPipeline(lmod, tm, opts.optLevel, opts.loops, opts);
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

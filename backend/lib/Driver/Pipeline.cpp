// Pipeline.cpp: parse, verify, MLIR passes, translate, link runtime, optimize,
// emit. Everything runs in-process. Each call owns its own MLIRContext and
// LLVMContext, so calls are safe to run in parallel.
#include "marmot/Driver/Pipeline.h"
#include "marmot/Driver/RuntimeBitcode.h"
#include "marmot/Driver/Targets.h"
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
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
#include "llvm/IR/Module.h"
#include "llvm/IR/Verifier.h"
#include "llvm/MC/TargetRegistry.h"
#include "llvm/Passes/PassBuilder.h"
#include "llvm/Passes/StandardInstrumentations.h"
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
  triple = opts.triple.empty() ? hostTriple()
                               : llvm::Triple(llvm::Triple::normalize(opts.triple));
  if (!initTargetFor(triple, err))
    return nullptr;
  const llvm::Target *target = llvm::TargetRegistry::lookupTarget(triple, err);
  if (!target)
    return nullptr;
  bool isHost = opts.triple.empty() ||
                triple.getArch() == hostTriple().getArch();
  std::string cpu = opts.cpu;
  std::string features = opts.features;
  if (cpu.empty() && isHost) {
    cpu = llvm::sys::getHostCPUName().str();
    if (features.empty()) {
      llvm::SubtargetFeatures f;
      for (auto &kv : llvm::sys::getHostCPUFeatures())
        f.AddFeature(kv.first(), kv.second);
      features = f.getString();
    }
  }
  llvm::TargetOptions to;
  to.FunctionSections = true;
  to.DataSections = true;
  return std::unique_ptr<llvm::TargetMachine>(target->createTargetMachine(
      triple, cpu, features, to, llvm::Reloc::PIC_, std::nullopt,
      codegenLevel(opts.optLevel)));
}

// Every function gets the target CPU and features of the target machine, as
// clang does. Without this the inliner refuses to inline runtime functions
// that were compiled with explicit target attributes.
void setTargetAttrs(llvm::Module &m, llvm::TargetMachine &tm) {
  llvm::StringRef cpu = tm.getTargetCPU();
  llvm::StringRef features = tm.getTargetFeatureString();
  for (llvm::Function &f : m) {
    if (f.isDeclaration())
      continue;
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

void runLLVMPipeline(llvm::Module &m, llvm::TargetMachine &tm, char level) {
  llvm::LoopAnalysisManager lam;
  llvm::FunctionAnalysisManager fam;
  llvm::CGSCCAnalysisManager cgam;
  llvm::ModuleAnalysisManager mam;
  llvm::PipelineTuningOptions pto;
  pto.LoopUnrolling = level != 's' && level != 'z';
  pto.LoopVectorization = level == '2' || level == '3';
  pto.SLPVectorization = level == '2' || level == '3';
  // Standard instrumentation makes -time-passes, -print-after and friends work.
  llvm::PassInstrumentationCallbacks pic;
  llvm::StandardInstrumentations si(m.getContext(), /*DebugLogging=*/false);
  si.registerCallbacks(pic, &mam);
  llvm::PassBuilder pb(&tm, pto, std::nullopt, &pic);
  pb.registerModuleAnalyses(mam);
  pb.registerCGSCCAnalyses(cgam);
  pb.registerFunctionAnalyses(fam);
  pb.registerLoopAnalyses(lam);
  pb.crossRegisterProxies(lam, fam, cgam, mam);
  llvm::ModulePassManager mpm;
  llvm::OptimizationLevel ol = passLevel(level);
  if (ol == llvm::OptimizationLevel::O0)
    mpm = pb.buildO0DefaultPipeline(ol);
  else
    mpm = pb.buildPerModuleDefaultPipeline(ol);
  mpm.run(m, mam);
}

} // namespace

void PhaseTimes::add(const PhaseTimes &o) {
  parse += o.parse;
  mlirPasses += o.mlirPasses;
  translate += o.translate;
  linkRt += o.linkRt;
  opt += o.opt;
  codegen += o.codegen;
  write += o.write;
}

void PhaseTimes::print(llvm::raw_ostream &os, const std::string &label) const {
  os << llvm::format("%-24s parse %7.2f  mlir %7.2f  translate %7.2f  link-rt %7.2f"
                     "  opt %7.2f  codegen %7.2f  write %6.2f  total %8.2f ms\n",
                     label.c_str(), parse, mlirPasses, translate, linkRt, opt,
                     codegen, write, total());
}

bool parseOptLevel(const std::string &s, char &level) {
  if (s.size() != 1 || std::string("0123sz").find(s[0]) == std::string::npos)
    return false;
  level = s[0];
  return true;
}

bool compileFile(const std::string &input, const std::string &output,
                 const CompileOptions &opts, PhaseTimes &times,
                 llvm::raw_ostream &errs) {
  PhaseTimer timer;
  auto fail = [&](const llvm::Twine &msg) {
    errs << input << ": " << msg << "\n";
    return false;
  };

  // Parse. The parser verifies the module once.
  mlir::DialectRegistry registry;
  registerDialects(registry);
  mlir::MLIRContext mctx(registry, opts.singleThreaded
                                       ? mlir::MLIRContext::Threading::DISABLED
                                       : mlir::MLIRContext::Threading::ENABLED);
  llvm::SourceMgr sm;
  std::string ioErr;
  auto inBuf = mlir::openInputFile(input, &ioErr);
  if (!inBuf)
    return fail(ioErr);
  sm.AddNewSourceBuffer(std::move(inBuf), llvm::SMLoc());
  mlir::SourceMgrDiagnosticHandler diag(sm, &mctx, errs);
  mlir::OwningOpRef<mlir::ModuleOp> module =
      mlir::parseSourceFile<mlir::ModuleOp>(sm, mlir::ParserConfig(&mctx));
  if (!module)
    return fail("failed to parse");
  times.parse += timer.lap();

  // MLIR passes: go dialect passes and lowering to the llvm dialect.
  {
    mlir::PassManager pm(&mctx, mlir::ModuleOp::getOperationName());
    pm.enableVerifier(opts.verifyEach || opts.optLevel == '0');
    if (opts.optLevel != '0') {
      if (opts.escape)
        pm.addPass(go::createGoEscape());
    }
    pm.addPass(go::createGoToLLVM());
    // Always: large aggregate values would hang code generation.
    pm.addPass(go::createAggregateCopies());
    if (mlir::failed(pm.run(*module)))
      return fail("MLIR pass pipeline failed");
  }
  times.mlirPasses += timer.lap();

  if (opts.emit == Emit::MLIR) {
    std::error_code ec;
    llvm::ToolOutputFile out(output, ec, llvm::sys::fs::OF_None);
    if (ec)
      return fail(output + ": " + ec.message());
    module->print(out.os());
    out.keep();
    times.write += timer.lap();
    return true;
  }

  // Target machine, translate.
  std::string err;
  llvm::Triple triple;
  std::unique_ptr<llvm::TargetMachine> tm = makeTargetMachine(opts, triple, err);
  if (!tm)
    return fail(err);
  llvm::LLVMContext lctx;
  std::unique_ptr<llvm::Module> lmod =
      mlir::translateModuleToLLVMIR(*module, lctx, input);
  if (!lmod)
    return fail("failed to translate to LLVM IR");
  lmod->setTargetTriple(triple);
  lmod->setDataLayout(tm->createDataLayout());
  module = nullptr; // free MLIR memory before the LLVM pipeline
  times.translate += timer.lap();

  // Runtime bitcode is only useful when the optimizer can inline it.
  if (opts.rtBitcode && opts.optLevel != '0') {
    if (!linkRuntimeBitcode(*lmod, *opts.rtBitcode, err))
      return fail(err);
  }
  setTargetAttrs(*lmod, *tm);
  if (opts.initOptNone && opts.optLevel != '0')
    markInitOptNone(*lmod);
  times.linkRt += timer.lap();

  if (opts.verifyEach && llvm::verifyModule(*lmod, &errs))
    return fail("LLVM IR verification failed");
  runLLVMPipeline(*lmod, *tm, opts.optLevel);
  times.opt += timer.lap();

  std::error_code ec;
  bool binary = opts.emit == Emit::Obj || opts.emit == Emit::BC;
  llvm::ToolOutputFile out(output, ec,
                           binary ? llvm::sys::fs::OF_None : llvm::sys::fs::OF_Text);
  if (ec)
    return fail(output + ": " + ec.message());
  switch (opts.emit) {
  case Emit::LLVM:
    lmod->print(out.os(), nullptr);
    break;
  case Emit::BC:
    llvm::WriteBitcodeToFile(*lmod, out.os());
    break;
  case Emit::Obj:
  case Emit::Asm: {
    llvm::SmallVector<char, 0> buf;
    {
      llvm::raw_svector_ostream bos(buf);
      llvm::legacy::PassManager cg;
      auto ft = opts.emit == Emit::Obj ? llvm::CodeGenFileType::ObjectFile
                                       : llvm::CodeGenFileType::AssemblyFile;
      if (tm->addPassesToEmitFile(cg, bos, nullptr, ft))
        return fail("target cannot emit this file type");
      cg.run(*lmod);
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

} // namespace marmot

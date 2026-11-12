// Gpu.cpp: GPU offload of parallel loop bodies. The PTX is produced in-process by LLVM's NVPTX
// target; no CUDA toolkit is needed at compile time.
#include "marmot/Driver/Gpu.h"
#include "marmot/Driver/Targets.h"

#include "llvm/ADT/SetVector.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/IntrinsicsNVPTX.h"
#include "llvm/IR/LegacyPassManager.h"
#include "llvm/IR/Module.h"
#include "llvm/MC/TargetRegistry.h"
#include "llvm/Passes/PassBuilder.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Target/TargetMachine.h"
#include "llvm/Target/TargetOptions.h"
#include "llvm/Transforms/Utils/BasicBlockUtils.h"
#include "llvm/Transforms/Utils/Cloning.h"
#include "llvm/Transforms/Utils/ModuleUtils.h"

#include "llvm/ADT/DenseSet.h"

#include <cstdlib>
#include <mutex>

extern "C" {
void LLVMInitializeNVPTXTargetInfo();
void LLVMInitializeNVPTXTarget();
void LLVMInitializeNVPTXTargetMC();
void LLVMInitializeNVPTXAsmPrinter();
}

using namespace llvm;

namespace marmot {

namespace {

constexpr StringLiteral kGpuBody = "marmot-gpu-body";

std::unique_ptr<TargetMachine> nvptxMachine(const std::string &arch, std::string &err) {
  static std::once_flag once;
  std::call_once(once, [] {
    LLVMInitializeNVPTXTargetInfo();
    LLVMInitializeNVPTXTarget();
    LLVMInitializeNVPTXTargetMC();
    LLVMInitializeNVPTXAsmPrinter();
  });
  Triple triple("nvptx64-nvidia-cuda");
  const Target *t = TargetRegistry::lookupTarget(triple, err);
  if (!t)
    return nullptr;
  TargetOptions to;
  to.AllowFPOpFusion = FPOpFusion::Strict; // Go: no contraction into fma
  return std::unique_ptr<TargetMachine>(t->createTargetMachine(
      triple, arch, "+ptx80", to, std::nullopt, std::nullopt, CodeGenOptLevel::Aggressive));
}

// Defined functions reachable by calls from root.
SetVector<Function *> callClosure(Function *root) {
  SetVector<Function *> out;
  SmallVector<Function *, 8> work{root};
  out.insert(root);
  while (!work.empty()) {
    Function *f = work.pop_back_val();
    for (BasicBlock &bb : *f)
      for (Instruction &inst : bb)
        if (auto *cb = dyn_cast<CallBase>(&inst))
          if (Function *c = cb->getCalledFunction())
            if (!c->isDeclaration() && out.insert(c))
              work.push_back(c);
  }
  return out;
}

// Returns the PTX for body, or an empty string with err set.
std::string compileKernel(Module &host, Function *body, const std::string &arch,
                          std::string &kname, std::string &err) {
  SetVector<Function *> need = callClosure(body);
  for (Function *f : need)
    for (BasicBlock &bb : *f)
      for (Instruction &inst : bb) {
        for (Value *op : inst.operands())
          if (isa<GlobalVariable>(op->stripPointerCasts())) {
            err = body->getName().str() + ": GPU body references a global variable";
            return "";
          }
        if (auto *cb = dyn_cast<CallBase>(&inst)) {
          Function *c = cb->getCalledFunction();
          if (!c || (c->isDeclaration() && !c->isIntrinsic())) {
            err = body->getName().str() + ": GPU body calls an external function";
            return "";
          }
        }
      }
  ValueToValueMapTy vmap;
  std::unique_ptr<Module> dev = CloneModule(host, vmap, [&](const GlobalValue *gv) {
    auto *f = dyn_cast<Function>(gv);
    return f && need.count(const_cast<Function *>(f));
  });
  auto tm = nvptxMachine(arch, err);
  if (!tm)
    return "";
  dev->setTargetTriple(tm->getTargetTriple());
  dev->setDataLayout(tm->createDataLayout());
  // Keep only the kernel's functions; drop host globals and attributes.
  for (GlobalVariable &v : make_early_inc_range(dev->globals()))
    if (v.use_empty())
      v.eraseFromParent();
  for (Function &f : make_early_inc_range(*dev)) {
    if (f.isDeclaration() && !f.isIntrinsic() && f.use_empty()) {
      f.eraseFromParent();
      continue;
    }
    f.removeFnAttr("target-cpu");
    f.removeFnAttr("target-features");
    f.removeFnAttr("frame-pointer");
    if (!f.isDeclaration())
      f.setLinkage(GlobalValue::InternalLinkage);
  }
  Function *dbody = cast<Function>(vmap[body]);
  // kernel(ctx, n): one thread per iteration.
  LLVMContext &ctx = dev->getContext();
  Type *i64 = Type::getInt64Ty(ctx);
  Type *ptr = PointerType::get(ctx, 0);
  kname = (body->getName() + ".kernel").str();
  for (char &c : kname)
    if (!isAlnum(c) && c != '_')
      c = '_';
  auto *kty = FunctionType::get(Type::getVoidTy(ctx), {ptr, i64}, false);
  Function *k = Function::Create(kty, GlobalValue::ExternalLinkage, kname, *dev);
  k->setCallingConv(CallingConv::PTX_Kernel);
  BasicBlock *entry = BasicBlock::Create(ctx, "entry", k);
  BasicBlock *run = BasicBlock::Create(ctx, "run", k);
  BasicBlock *done = BasicBlock::Create(ctx, "done", k);
  IRBuilder<> b(entry);
  auto sreg = [&](Intrinsic::ID id) {
    return b.CreateZExt(b.CreateIntrinsic(id, ArrayRef<Type *>{}, ArrayRef<Value *>{}), i64);
  };
  Value *bid = sreg(Intrinsic::nvvm_read_ptx_sreg_ctaid_x);
  Value *bdim = sreg(Intrinsic::nvvm_read_ptx_sreg_ntid_x);
  Value *tid = sreg(Intrinsic::nvvm_read_ptx_sreg_tid_x);
  Value *i = b.CreateAdd(b.CreateMul(bid, bdim), tid);
  b.CreateCondBr(b.CreateICmpULT(i, k->getArg(1)), run, done);
  b.SetInsertPoint(run);
  b.CreateCall(dbody, {k->getArg(0), i, b.CreateAdd(i, ConstantInt::get(i64, 1))});
  b.CreateBr(done);
  b.SetInsertPoint(done);
  b.CreateRetVoid();
  dbody->removeFnAttr(kGpuBody);

  // Optimize and emit PTX.
  {
    LoopAnalysisManager lam;
    FunctionAnalysisManager fam;
    CGSCCAnalysisManager cgam;
    ModuleAnalysisManager mam;
    PassBuilder pb(tm.get());
    pb.registerModuleAnalyses(mam);
    pb.registerCGSCCAnalyses(cgam);
    pb.registerFunctionAnalyses(fam);
    pb.registerLoopAnalyses(lam);
    pb.crossRegisterProxies(lam, fam, cgam, mam);
    pb.buildPerModuleDefaultPipeline(OptimizationLevel::O3).run(*dev, mam);
  }
  SmallString<0> ptx;
  {
    raw_svector_ostream os(ptx);
    legacy::PassManager cg;
    if (tm->addPassesToEmitFile(cg, os, nullptr, CodeGenFileType::AssemblyFile)) {
      err = "NVPTX cannot emit PTX";
      return "";
    }
    cg.run(*dev);
  }
  return std::string(ptx);
}

Constant *cstring(Module &m, StringRef data, const Twine &name) {
  Constant *init = ConstantDataArray::getString(m.getContext(), data, /*AddNull=*/true);
  auto *g = new GlobalVariable(m, init->getType(), true, GlobalValue::PrivateLinkage, init,
                               name);
  g->setUnnamedAddr(GlobalValue::UnnamedAddr::Global);
  g->setAlignment(Align(8));
  return g;
}

} // namespace

bool lowerGpuLoops(Module &m, const std::string &gpuArch, std::string &err) {
  Function *marker = m.getFunction("marmot_gpu_for");
  if (!marker)
    return true;
  LLVMContext &ctx = m.getContext();
  Type *i64 = Type::getInt64Ty(ctx);
  Type *ptr = PointerType::get(ctx, 0);
  // rt_gpu_for(ptx, ptxlen, kname, n, ctx, ctxsize, ranges, nranges, costPerIter) -> i1
  FunctionCallee gpuFor = m.getOrInsertFunction(
      "rt_gpu_for", FunctionType::get(Type::getInt1Ty(ctx),
                                      {ptr, i64, ptr, i64, ptr, i64, ptr, i64, i64}, false));
  DenseMap<Function *, std::pair<Constant *, std::pair<Constant *, uint64_t>>> compiled;
  DenseSet<Function *> failed;

  SmallVector<CallBase *, 8> calls;
  for (Use &u : marker->uses())
    if (auto *cb = dyn_cast<CallBase>(u.getUser()); cb && cb->isCallee(&u))
      calls.push_back(cb);
  for (CallBase *cb : calls) {
    if (cb->arg_size() != 7 || !cb->getType()->isIntegerTy(1)) {
      err = "marmot_gpu_for is i1 (body, n, ctx, ctxsize, ranges, nranges, costPerIter)";
      return false;
    }
    auto *body = dyn_cast<Function>(cb->getArgOperand(0)->stripPointerCasts());
    bool gpu = !gpuArch.empty() && body && body->hasFnAttribute(kGpuBody) &&
               !failed.count(body);
    if (gpu && !compiled.count(body)) {
      std::string kname, kerr;
      std::string ptx = compileKernel(m, body, gpuArch, kname, kerr);
      if (ptx.empty()) {
        failed.insert(body); // stays on the CPU path
        gpu = false;
        if (getenv("MARMOT_GPU_REMARKS"))
          errs() << "gpu: " << kerr << "\n";
      } else {
        compiled[body] = {cstring(m, ptx, body->getName() + ".ptx"),
                          {cstring(m, kname, body->getName() + ".kname"), ptx.size() + 1}};
      }
    }
    Value *repl;
    if (gpu) {
      IRBuilder<> b(cb);
      auto &[ptxG, kn] = compiled[body];
      repl = b.CreateCall(gpuFor, {ptxG, ConstantInt::get(i64, kn.second), kn.first,
                                   cb->getArgOperand(1), cb->getArgOperand(2),
                                   cb->getArgOperand(3), cb->getArgOperand(4),
                                   cb->getArgOperand(5), cb->getArgOperand(6)});
    } else {
      repl = ConstantInt::getFalse(ctx); // the caller's CPU path runs
    }
    cb->replaceAllUsesWith(repl);
    cb->eraseFromParent();
  }
  // rtcore loads registered modules on its startup thread, so the first rt_gpu_for finds the
  // kernel compiled: one constructor per module calls rt_gpu_register(ptx, ptxlen) for each
  // embedded image, with the same pointer the calls pass.
  if (!compiled.empty()) {
    FunctionCallee reg = m.getOrInsertFunction(
        "rt_gpu_register", FunctionType::get(Type::getVoidTy(ctx), {ptr, i64}, false));
    auto *ctor = Function::Create(FunctionType::get(Type::getVoidTy(ctx), false),
                                  GlobalValue::InternalLinkage, "marmot.gpu.register", m);
    IRBuilder<> b(BasicBlock::Create(ctx, "entry", ctor));
    SmallVector<std::pair<Constant *, uint64_t>, 4> images;
    for (auto &[body, c] : compiled)
      images.push_back({c.first, c.second.second});
    llvm::sort(images, [](auto &x, auto &y) {
      return x.first->getName() < y.first->getName(); // deterministic order
    });
    for (auto &[img, len] : images)
      b.CreateCall(reg, {img, ConstantInt::get(i64, len)});
    b.CreateRetVoid();
    appendToGlobalCtors(m, ctor, 65535);
  }
  for (Function &f : m)
    f.removeFnAttr(kGpuBody);
  if (marker->use_empty())
    marker->eraseFromParent();
  return true;
}

} // namespace marmot

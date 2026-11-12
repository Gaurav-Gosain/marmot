// Multiversion.cpp: x86-64 versions of chunk functions (rule X).
//
// A portable x86-64 binary targets the baseline (or v2) instruction set. For
// every chunk function F$par that rt_parallel_for runs, this step adds copies
// for x86-64-v3 (AVX2) and, on request, x86-64-v4 (AVX-512), and the caller
// picks one at run time:
//
//     lvl = rt_x86_level()          ; psABI level, computed once at startup
//     body = lvl >= 4 ? F$par.v4 : lvl >= 3 ? F$par.v3 : F$par
//     rt_parallel_for(tc, grain, body, ctx)
//
// The copies are made right after Rule P, before the function pipeline, so
// each one is vectorized for its own target. They differ only in the
// "target-cpu" attribute: the IR, and so the result of every operation, is
// the same. LLVM contracts a*b+c into an FMA only under fast-math flags or
// llvm.fmuladd, which the frontend never emits on amd64 (gc does not fuse
// there), so the v3 and v4 copies round exactly as the baseline does.
//
// Only modules whose functions target a baseline CPU get copies: a build
// with -mcpu=native or an explicit newer CPU already has the wide units.

#include "Internal.h"

#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/Module.h"
#include "llvm/Support/CommandLine.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/TargetParser/Triple.h"
#include "llvm/Transforms/Utils/Cloning.h"

using namespace llvm;

namespace marmot {
namespace loops {

static cl::opt<unsigned> MVLevel(
    "loops-mv", cl::Hidden, cl::init(0),
    cl::desc("Loop optimizer: x86-64 chunk function versions up to this psABI level "
             "(3: AVX2, 4: AVX2 and AVX-512; 0: off)"));

static bool baselineCPU(const Function &F) {
  StringRef cpu = F.getFnAttribute("target-cpu").getValueAsString();
  return cpu.empty() || cpu == "x86-64" || cpu == "x86-64-v2" || cpu == "generic";
}

bool multiversion(Module &M, const Options &opts) {
  unsigned top = MVLevel;
  if (top < 3 || !Triple(M.getTargetTriple()).isX86_64())
    return false;
  top = std::min(top, 4u);
  Function *rpf = M.getFunction("rt_parallel_for");
  if (!rpf)
    return false;
  LLVMContext &C = M.getContext();
  Type *i32 = Type::getInt32Ty(C);
  // The level does not change after startup: the call reads no memory the
  // program can write, so LLVM may hoist and merge it.
  FunctionCallee lvlFn = M.getOrInsertFunction("rt_x86_level", FunctionType::get(i32, false));
  if (auto *lf = dyn_cast<Function>(lvlFn.getCallee())) {
    lf->setDoesNotThrow();
    lf->setWillReturn();
    lf->setMemoryEffects(MemoryEffects::none());
  }
  DenseMap<Function *, SmallVector<Function *, 2>> copies;
  bool changed = false;
  for (User *u : make_early_inc_range(rpf->users())) {
    auto *call = dyn_cast<CallInst>(u);
    if (!call || call->getCalledFunction() != rpf)
      continue;
    auto *body = dyn_cast<Function>(call->getArgOperand(2));
    if (!body || !body->hasFnAttribute("marmot-par-body") || !baselineCPU(*body))
      continue;
    auto &vs = copies[body];
    if (vs.empty())
      for (unsigned lvl = 3; lvl <= top; lvl++) {
        ValueToValueMapTy vmap;
        Function *nf = CloneFunction(body, vmap);
        // CloneFunction puts the copy in the module; name it after its level.
        nf->setName(body->getName() + ".v" + Twine(lvl));
        nf->addFnAttr("target-cpu", "x86-64-v" + std::to_string(lvl));
        vs.push_back(nf);
      }
    IRBuilder<> b(call);
    Value *lvl = b.CreateCall(lvlFn, {}, "x86.level");
    Value *pick = body;
    for (unsigned i = 0; i < vs.size(); i++)
      pick = b.CreateSelect(b.CreateICmpSGE(lvl, ConstantInt::get(i32, 3 + i)), vs[i], pick,
                            "par.body");
    call->setArgOperand(2, pick);
    changed = true;
    if (opts.remarks)
      errs() << "loops: " << call->getFunction()->getName() << ": " << body->getName()
             << ": x86-64 versions up to v" << top << "\n";
  }
  return changed;
}

unsigned mvLevelOption() { return MVLevel; }

} // namespace loops
} // namespace marmot

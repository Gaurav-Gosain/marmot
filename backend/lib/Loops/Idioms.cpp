// Idioms.cpp: loop idioms replaced by runtime kernels (Rule D in
// docs/loops.md).
//
// Byte count: an innermost counted loop whose only work is
//
//     acc += (p[i] == c)        (or p[i] != c), i = 0, 1, ..., n-1
//
// over consecutive bytes becomes acc += rt_count_byte(p, n, c) (or
// acc += n - rt_count_byte(p, n, c)). rt_count_byte is rtcore's SIMD
// bytealg.Count. The loop reads exactly the bytes the call reads (its own
// bounds checks were proved or versioned away, since the loop has no other
// exit), integer addition wraps, so the result is the same.

#include "Internal.h"

#include "llvm/Analysis/AssumptionCache.h"
#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/IntrinsicInst.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/PassManager.h"
#include "llvm/IR/PatternMatch.h"
#include "llvm/IR/Verifier.h"
#include "llvm/Support/ErrorHandling.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Transforms/Utils/BasicBlockUtils.h"
#include "llvm/Transforms/Utils/LoopSimplify.h"
#include "llvm/Transforms/Utils/LoopUtils.h"
#include "llvm/Transforms/Utils/ScalarEvolutionExpander.h"

using namespace llvm;
using namespace llvm::PatternMatch;

namespace marmot {
namespace loops {
namespace {

class IdiomPass : public PassInfoMixin<IdiomPass> {
public:
  explicit IdiomPass(Options o) : opts(o) {}
  PreservedAnalyses run(Function &F, FunctionAnalysisManager &FAM);

private:
  Options opts;
  bool byteCount(Function &F, Loop *L, ScalarEvolution &SE, LoopInfo &LI, DominatorTree &DT);
};

bool IdiomPass::byteCount(Function &F, Loop *L, ScalarEvolution &SE, LoopInfo &LI,
                          DominatorTree &DT) {
  auto no = [&](int why) {
    if (std::getenv("MARMOT_LOOPS_DEBUG"))
      errs() << "loops: idiom: " << F.getName() << " loop " << L->getHeader()->getName()
             << " rejected at check " << why << "\n";
    return false;
  };
  if (!L->getSubLoops().empty() || !L->isLoopSimplifyForm())
    return no(1);
  // Values used after the loop must go through exit phis for the checks
  // below; the function pipeline does not guarantee LCSSA here.
  formLCSSA(*L, DT, &LI, &SE);
  BasicBlock *latch = L->getLoopLatch(), *pre = L->getLoopPreheader();
  BasicBlock *exit = L->getExitBlock();
  if (L->getExitingBlock() != latch || !exit)
    return no(2);
  const SCEV *btc = SE.getBackedgeTakenCount(L);
  if (isa<SCEVCouldNotCompute>(btc) || btc->getType()->getIntegerBitWidth() > 64)
    return no(3);
  // Header phis: one induction (not used after L), one integer accumulator.
  PHINode *acc = nullptr;
  for (PHINode &p : L->getHeader()->phis()) {
    auto *ar = dyn_cast<SCEVAddRecExpr>(SE.getSCEV(&p));
    if (ar && ar->getLoop() == L && ar->isAffine())
      continue; // an induction; checked through the users below
    if (acc || !p.getType()->isIntegerTy())
      return no(4);
    acc = &p;
  }
  if (!acc)
    return no(5);
  // acc' = acc + zext(icmp eq/ne (load i8 p[i]), c)
  Value *next = acc->getIncomingValueForBlock(latch);
  Value *inc = nullptr;
  if (!match(next, m_c_Add(m_Specific(acc), m_Value(inc))))
    return no(6);
  Value *cmpv = nullptr;
  if (!match(inc, m_ZExt(m_Value(cmpv))) &&
      !match(inc, m_Select(m_Value(cmpv), m_One(), m_Zero())))
    return no(7);
  auto *cmp = dyn_cast<ICmpInst>(cmpv);
  if (!cmp || !cmp->isEquality())
    return no(8);
  LoadInst *ld = dyn_cast<LoadInst>(cmp->getOperand(0));
  Value *c = cmp->getOperand(1);
  if (!ld) {
    ld = dyn_cast<LoadInst>(cmp->getOperand(1));
    c = cmp->getOperand(0);
  }
  if (!ld || !ld->isSimple() || !ld->getType()->isIntegerTy(8) || !L->isLoopInvariant(c) ||
      !L->contains(ld))
    return no(9);
  auto *addr = dyn_cast<SCEVAddRecExpr>(SE.getSCEV(ld->getPointerOperand()));
  if (!addr || addr->getLoop() != L || !addr->isAffine() || !addr->getStepRecurrence(SE)->isOne())
    return no(10);
  // Nothing else in L: no memory writes, no calls; every value of L but the
  // accumulator stays inside L.
  for (BasicBlock *bb : L->blocks())
    for (Instruction &I : *bb) {
      if (&I != ld && (I.mayReadOrWriteMemory() || isa<CallBase>(I)))
        return no(11);
      for (User *u : I.users()) {
        auto *ui = cast<Instruction>(u);
        if (L->contains(ui))
          continue;
        if (&I != next || !isa<PHINode>(ui) || ui->getParent() != exit)
          return no(12);
      }
    }
  // The accumulator is used only by its own update.
  for (User *u : acc->users())
    if (u != next)
      return no(13);
  SCEVExpander exp(SE, "idiom");
  Instruction *at = pre->getTerminator();
  if (!exp.isSafeToExpandAt(addr->getStart(), at) || !exp.isSafeToExpandAt(btc, at))
    return no(14);

  // ---- transform ----
  LLVMContext &C = F.getContext();
  Type *i64 = Type::getInt64Ty(C);
  Value *p0 = exp.expandCodeFor(addr->getStart(), nullptr, at);
  Value *n = exp.expandCodeFor(btc, i64, at);
  IRBuilder<> b(at);
  n = b.CreateAdd(n, ConstantInt::get(i64, 1), "idiom.n");
  Module &M = *F.getParent();
  FunctionCallee fn = M.getOrInsertFunction(
      "rt_count_byte", FunctionType::get(i64, {PointerType::getUnqual(C), i64, Type::getInt8Ty(C)},
                                         false));
  if (auto *callee = dyn_cast<Function>(fn.getCallee())) {
    callee->addParamAttr(2, Attribute::ZExt);
    callee->setOnlyReadsMemory();
    callee->setDoesNotThrow();
  }
  CallInst *call = b.CreateCall(fn, {p0, n, c}, "idiom.count");
  call->addParamAttr(2, Attribute::ZExt);
  Value *cnt = call;
  if (cmp->getPredicate() == ICmpInst::ICMP_NE)
    cnt = b.CreateSub(n, cnt);
  Value *init = acc->getIncomingValueForBlock(pre);
  Value *res = b.CreateAdd(init, b.CreateZExtOrTrunc(cnt, acc->getType()), "idiom.acc");
  // Skip the loop.
  // The exit phis get the result from the preheader; deleting the loop
  // below removes their latch entries.
  for (PHINode &p : exit->phis())
    p.addIncoming(res, pre);
  BranchInst::Create(exit, at->getIterator());
  at->eraseFromParent();
  SmallVector<BasicBlock *, 8> dead(L->blocks().begin(), L->blocks().end());
  DeleteDeadBlocks(dead);
  if (opts.remarks)
    errs() << "loops: " << F.getName() << ": byte count loop replaced by rt_count_byte\n";
  return true;
}

PreservedAnalyses IdiomPass::run(Function &F, FunctionAnalysisManager &FAM) {
  if (F.isDeclaration() || F.hasOptNone())
    return PreservedAnalyses::all();
  bool changed = false;
  for (int round = 0; round < 16; round++) {
    auto &LI = FAM.getResult<LoopAnalysis>(F);
    auto &SE = FAM.getResult<ScalarEvolutionAnalysis>(F);
    auto &DT = FAM.getResult<DominatorTreeAnalysis>(F);
    auto &AC = FAM.getResult<AssumptionAnalysis>(F);
    for (Loop *L : LI)
      simplifyLoop(L, &DT, &LI, &SE, &AC, nullptr, /*PreserveLCSSA=*/false);
    bool did = false;
    for (Loop *L : LI.getLoopsInPreorder())
      if (byteCount(F, L, SE, LI, DT)) {
        did = true;
        break;
      }
    if (!did)
      break;
    changed = true;
    FAM.invalidate(F, PreservedAnalyses::none());
    if (verifyFunction(F, &errs()))
      report_fatal_error("loops: idiom replacement produced invalid IR in " + F.getName());
  }
  // LCSSA formation may have added phis even without a replacement.
  (void)changed;
  return PreservedAnalyses::none();
}

} // namespace

void addIdioms(ModulePassManager &mpm, const Options &opts) {
  mpm.addPass(createModuleToFunctionPassAdaptor(IdiomPass(opts)));
}

} // namespace loops
} // namespace marmot

// MaskedJam.cpp: masked jam, outer loop vectorization of loops whose inner
// loop has a data dependent trip count (rule M).
//
// For a loop X whose body is   Fore; W (inner loop, data dependent exits); Aft
// the main part of X runs U iterations at once:
//
//     Fore(x), ..., Fore(x+U-1)
//     joint W: every lane steps its own copy of W's state; when a lane
//              takes an exit it records its exit values (selects on its
//              active mask); a lane that left keeps stepping, its values are
//              no longer used; the joint loop runs until no lane is active
//     Aft(x), ..., Aft(x+U-1)     in order, with each lane's exit values
//
// and the remaining TC % U iterations run in the original loop. The lanes'
// arithmetic is independent, so the SLP vectorizer packs it into vector
// registers (mandelbrot: U pixels per escape iteration).
//
// Legal because Fore and W have no memory accesses, no calls with effects and
// nothing that can trap, and do not use values X carries between iterations
// (except its induction): so running Fore and W of later iterations before
// Aft of earlier ones changes nothing, Aft runs in the original order, and
// every lane computes exactly the values the original iteration computes.

#include "Internal.h"

#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/IntrinsicInst.h"
#include "llvm/IR/PassManager.h"
#include "llvm/IR/Verifier.h"
#include "llvm/Support/ErrorHandling.h"
#include "llvm/Support/CommandLine.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Transforms/Scalar/SimplifyCFG.h"
#include "llvm/Transforms/Utils/LCSSA.h"
#include "llvm/Transforms/Utils/LoopSimplify.h"
#include "llvm/Transforms/Utils/Cloning.h"
#include "llvm/Transforms/Utils/LoopUtils.h"
#include "llvm/Transforms/Utils/ScalarEvolutionExpander.h"

using namespace llvm;

namespace marmot {
namespace loops {
namespace {

constexpr unsigned MaxLanes = 8;
} // namespace
static cl::opt<unsigned> MJLanes("loops-mj-lanes", cl::Hidden, cl::init(8),
                                 cl::desc("Masked jam: lanes per joint loop"));
static cl::opt<bool> MJVector("loops-mj-vector", cl::Hidden, cl::init(false),
                              cl::desc("Masked jam: vector joint loop"));
namespace {

// Straight chain of blocks from `from` to `to` inclusive, all with one
// successor except `to`. Empty when the region is not a chain.
SmallVector<BasicBlock *, 8> chainOf(BasicBlock *from, BasicBlock *to, unsigned limit) {
  SmallVector<BasicBlock *, 8> c;
  for (BasicBlock *bb = from;;) {
    c.push_back(bb);
    if (bb == to)
      return c;
    if (c.size() > limit)
      return {};
    bb = bb->getSingleSuccessor();
    if (!bb)
      return {};
  }
}

// Instructions that are safe to compute for a lane that has already left:
// no memory, no calls with effects, nothing that traps.
bool speculatable(Instruction &I) {
  if (isa<PHINode>(I) || I.isTerminator())
    return true;
  if (I.mayReadOrWriteMemory() || I.mayThrow() || isa<AllocaInst>(I))
    return false;
  if (auto *cb = dyn_cast<CallBase>(&I))
    return cb->doesNotAccessMemory() && cb->willReturn() && !cb->isConvergent();
  switch (I.getOpcode()) {
  case Instruction::SDiv:
  case Instruction::UDiv:
  case Instruction::SRem:
  case Instruction::URem: {
    auto *c = dyn_cast<ConstantInt>(I.getOperand(1));
    return c && !c->isZero() && !c->isMinusOne();
  }
  default:
    return true;
  }
}

class MaskedJammer {
public:
  MaskedJammer(Function &F, LoopInfo &LI, ScalarEvolution &SE, const Options &opts)
      : F(F), LI(LI), SE(SE), opts(opts),
        Lanes(std::min<unsigned>(std::max<unsigned>(2, MJLanes), MaxLanes)) {}
  bool tryLoop(Loop *X);

private:
  Function &F;
  LoopInfo &LI;
  ScalarEvolution &SE;
  const Options &opts;
  unsigned Lanes; // a power of two
  std::string why;
  bool reject(const Twine &m) {
    why = m.str();
    return false;
  }
  bool apply(Loop *X);
  bool widenable(ArrayRef<BasicBlock *> wchain, ArrayRef<PHINode *> wphis,
                 ArrayRef<PHINode *> ephis);
  void buildScalarW(BasicBlock *mh, BasicBlock *mw, BasicBlock *ma, ArrayRef<BasicBlock *> wchain,
                    ArrayRef<PHINode *> wphis, ArrayRef<PHINode *> ephis, BasicBlock *wpre,
                    BasicBlock *wl, BasicBlock *E, ValueToValueMapTy *lane,
                    SmallVectorImpl<SmallVector<Value *, 8>> &res);
  void buildVectorW(BasicBlock *mh, BasicBlock *mw, BasicBlock *ma, ArrayRef<BasicBlock *> wchain,
                    ArrayRef<PHINode *> wphis, ArrayRef<PHINode *> ephis, BasicBlock *wpre,
                    BasicBlock *wl, BasicBlock *E, ValueToValueMapTy *lane,
                    SmallVectorImpl<SmallVector<Value *, 8>> &res);
};

bool MaskedJammer::tryLoop(Loop *X) {
  if (X->getSubLoops().size() != 1 || !X->getSubLoops()[0]->getSubLoops().empty())
    return false;
  bool ok = apply(X);
  if (opts.remarks && (ok || !why.empty()))
    errs() << "loops: " << F.getName() << ": masked jam loop " << X->getHeader()->getName()
           << ": " << (ok ? "done" : "no: " + why) << "\n";
  return ok;
}

bool MaskedJammer::apply(Loop *X) {
  Loop *W = X->getSubLoops()[0];
  if (!X->isLoopSimplifyForm() || !W->isLoopSimplifyForm())
    return reject("not in simplified form");
  BasicBlock *xh = X->getHeader(), *xl = X->getLoopLatch(), *xpre = X->getLoopPreheader();
  BasicBlock *xexit = X->getExitBlock();
  if (X->getExitingBlock() != xl || !xexit)
    return reject("outer loop exits elsewhere than its latch");
  auto *xbr = dyn_cast<BranchInst>(xl->getTerminator());
  if (!xbr || !xbr->isConditional())
    return reject("outer latch");
  BasicBlock *wh = W->getHeader(), *wl = W->getLoopLatch(), *wpre = W->getLoopPreheader();
  BasicBlock *E = W->getUniqueExitBlock();
  if (!E) {
    if (std::getenv("MARMOT_LOOPS_DEBUG")) {
      SmallVector<BasicBlock *, 4> ex;
      W->getExitBlocks(ex);
      for (BasicBlock *e : ex)
        errs() << "exit block " << e->getName() << "\n";
      F.print(errs());
    }
    return reject("inner loop has more than one exit block");
  }
  // Inner loop with a data dependent trip count is the point; a counted one
  // is loop-jam's job.
  if (!isa<SCEVCouldNotCompute>(SE.getBackedgeTakenCount(W)))
    return reject("inner loop is counted");

  // Fore: X header .. W preheader; W body: W header .. W latch; Aft: E .. X latch.
  auto fore = chainOf(xh, wpre, 16);
  auto body = chainOf(wh, wl, 32);
  auto aft = chainOf(E, xl, 16);
  if (fore.empty() || aft.empty())
    return reject("outer loop body has branches");
  // W's chain: every block but the latch has one in-loop successor and may
  // exit to E.
  SmallVector<BasicBlock *, 16> wchain;
  for (BasicBlock *bb = wh;;) {
    wchain.push_back(bb);
    if (wchain.size() > 32)
      return reject("inner loop too long");
    auto *br = dyn_cast<BranchInst>(bb->getTerminator());
    if (!br)
      return reject("inner terminator");
    if (bb == wl) {
      if (!br->isConditional() || (br->getSuccessor(0) != E && br->getSuccessor(1) != E))
        return reject("inner latch is not an exit");
      break;
    }
    BasicBlock *next = nullptr;
    if (br->isConditional()) {
      BasicBlock *a = br->getSuccessor(0), *b = br->getSuccessor(1);
      if (a == E && W->contains(b))
        next = b;
      else if (b == E && W->contains(a))
        next = a;
      else
        return reject("branch inside the inner loop");
    } else {
      next = br->getSuccessor(0);
    }
    bb = next;
  }
  if (wchain.size() != W->getNumBlocks())
    return reject("inner loop is not a chain");
  (void)body;

  // Purity of Fore and W.
  for (BasicBlock *bb : fore)
    for (Instruction &I : *bb)
      if (!speculatable(I))
        return reject(Twine("outer body before the inner loop: ") + I.getOpcodeName());
  for (BasicBlock *bb : wchain)
    for (Instruction &I : *bb)
      if (!speculatable(I))
        return reject(Twine("inner loop: ") + I.getOpcodeName());
  // Nothing in X outside Fore, W, Aft.
  if (fore.size() + wchain.size() + aft.size() != X->getNumBlocks())
    return reject("outer loop has other blocks");

  // X header phis: the induction, and carried values used only in Aft.
  const SCEV *xbtc = SE.getBackedgeTakenCount(X);
  if (isa<SCEVCouldNotCompute>(xbtc))
    return reject("outer trip count");
  PHINode *iv = nullptr;
  Value *ivStart = nullptr, *ivStep = nullptr;
  SmallVector<PHINode *, 4> carried;
  SmallPtrSet<BasicBlock *, 16> aftSet(aft.begin(), aft.end());
  for (PHINode &p : xh->phis()) {
    auto *next = dyn_cast<BinaryOperator>(p.getIncomingValueForBlock(xl));
    if (!iv && p.getType()->isIntegerTy() && next && next->getOpcode() == Instruction::Add &&
        (next->getOperand(0) == &p || next->getOperand(1) == &p)) {
      Value *step = next->getOperand(0) == &p ? next->getOperand(1) : next->getOperand(0);
      if (X->isLoopInvariant(step)) {
        iv = &p;
        ivStart = p.getIncomingValueForBlock(xpre);
        ivStep = step;
        continue;
      }
    }
    for (User *u : p.users()) {
      auto *ui = cast<Instruction>(u);
      if (X->contains(ui) && !aftSet.count(ui->getParent()))
        return reject("a carried value is used before the inner loop");
    }
    auto *lv = dyn_cast<Instruction>(p.getIncomingValueForBlock(xl));
    if (lv && X->contains(lv) && !aftSet.count(lv->getParent()))
      return reject("a carried value is computed before the inner loop");
    carried.push_back(&p);
  }
  if (!iv)
    return reject("no outer induction");
  // X's values used after X: only carried values' next values.
  for (BasicBlock *bb : X->blocks())
    for (Instruction &I : *bb)
      for (User *u : I.users()) {
        auto *ui = cast<Instruction>(u);
        if (X->contains(ui))
          continue;
        bool ok = false;
        for (PHINode *c : carried)
          if (c->getIncomingValueForBlock(xl) == &I)
            ok = true;
        if (!ok || !isa<PHINode>(ui) || ui->getParent() != xexit) {
          std::string t;
          raw_string_ostream os(t);
          I.print(os);
          os << "  used by ";
          ui->print(os);
          return reject("a value of the outer loop is used after it: " + StringRef(t).trim());
        }
      }
  // Fore and W values used after W: only through E's phis or in Aft.
  for (BasicBlock *bb : fore)
    for (Instruction &I : *bb)
      for (User *u : I.users()) {
        auto *ui = cast<Instruction>(u);
        if (!X->contains(ui))
          return reject("value of the outer body used after the loop");
      }
  for (BasicBlock *bb : wchain)
    for (Instruction &I : *bb)
      for (User *u : I.users()) {
        auto *ui = cast<Instruction>(u);
        if (W->contains(ui))
          continue;
        if (!(isa<PHINode>(ui) && ui->getParent() == E))
          return reject("inner value used after the inner loop other than by an exit phi");
      }
  SCEVExpander exp(SE, "mj");
  if (!exp.isSafeToExpandAt(xbtc, xpre->getTerminator()))
    return reject("trip count not expandable");

  // ---- transform ----
  LLVMContext &C = F.getContext();
  Type *i64 = Type::getInt64Ty(C);
  Type *i1 = Type::getInt1Ty(C);
  Instruction *at = xpre->getTerminator();
  Value *btc = exp.expandCodeFor(xbtc, i64, at);
  IRBuilder<> b(at);
  Value *tc = b.CreateAdd(btc, ConstantInt::get(i64, 1), "mj.tc");
  Value *main = b.CreateAnd(tc, ConstantInt::get(i64, ~uint64_t(Lanes - 1)), "mj.main");

  BasicBlock *mh = BasicBlock::Create(C, "mj.header", &F, xh);
  BasicBlock *mw = BasicBlock::Create(C, "mj.w", &F, xh);
  BasicBlock *ma = BasicBlock::Create(C, "mj.aft", &F, xh);
  BasicBlock *rpre = BasicBlock::Create(C, "mj.rest", &F, xh);
  BasicBlock *skip = BasicBlock::Create(C, "mj.skip", &F, xh);
  b.CreateCondBr(b.CreateICmpNE(main, ConstantInt::get(i64, 0)), mh, rpre);
  at->eraseFromParent();

  // Header: iteration counter and carried values.
  IRBuilder<> hb(mh);
  PHINode *n = hb.CreatePHI(i64, 2, "mj.n");
  n->addIncoming(ConstantInt::get(i64, 0), xpre);
  SmallVector<PHINode *, 4> cphi;
  for (PHINode *c : carried) {
    PHINode *p = hb.CreatePHI(c->getType(), 2, c->getName() + ".mj");
    p->addIncoming(c->getIncomingValueForBlock(xpre), xpre);
    cphi.push_back(p);
  }
  // Fore per lane.
  ValueToValueMapTy lane[MaxLanes];
  for (unsigned u = 0; u < Lanes; u++) {
    Value *k = hb.CreateAdd(n, ConstantInt::get(i64, u));
    Value *kk = hb.CreateSExtOrTrunc(k, ivStep->getType());
    lane[u][iv] = hb.CreateAdd(ivStart, hb.CreateMul(ivStep, kk), "mj.iv");
    for (BasicBlock *bb : fore)
      for (Instruction &I : *bb) {
        if (isa<PHINode>(I) || I.isTerminator())
          continue;
        Instruction *c = I.clone();
        hb.Insert(c);
        RemapInstruction(c, lane[u], RF_IgnoreMissingLocals | RF_NoModuleLevelChanges);
        lane[u][&I] = c;
      }
  }
  hb.CreateBr(mw);

  // Joint W loop: vector form when every instruction of W has one,
  // otherwise one scalar copy per lane.
  SmallVector<PHINode *, 8> wphis;
  for (PHINode &p : wh->phis())
    wphis.push_back(&p);
  SmallVector<PHINode *, 8> ephis;
  for (PHINode &p : E->phis())
    ephis.push_back(&p);
  SmallVector<SmallVector<Value *, 8>, MaxLanes> res(Lanes);
  bool vec = MJVector && widenable(wchain, wphis, ephis);
  if (vec)
    buildVectorW(mh, mw, ma, wchain, wphis, ephis, wpre, wl, E, lane, res);
  else
    buildScalarW(mh, mw, ma, wchain, wphis, ephis, wpre, wl, E, lane, res);

  // Aft per lane, in order.
  IRBuilder<> ab(ma);
  SmallVector<Value *, 4> cur(cphi.begin(), cphi.end());
  for (unsigned u = 0; u < Lanes; u++) {
    ValueToValueMapTy &vm = lane[u];
    // Values from the joint loop are only reachable through the res phis;
    // the lane's Fore values dominate ma.
    for (size_t i = 0; i < ephis.size(); i++)
      vm[ephis[i]] = res[u][i];
    for (size_t i = 0; i < carried.size(); i++)
      vm[carried[i]] = cur[i];
    for (BasicBlock *bb : aft)
      for (Instruction &I : *bb) {
        if (isa<PHINode>(I) || I.isTerminator())
          continue;
        Instruction *c = I.clone();
        ab.Insert(c);
        RemapInstruction(c, vm, RF_IgnoreMissingLocals | RF_NoModuleLevelChanges);
        vm[&I] = c;
      }
    for (size_t i = 0; i < carried.size(); i++) {
      Value *nv = carried[i]->getIncomingValueForBlock(xl);
      auto it = vm.find(nv);
      cur[i] = it != vm.end() ? static_cast<Value *>(it->second) : nv;
    }
  }
  Value *n2 = ab.CreateAdd(n, ConstantInt::get(i64, Lanes));
  n->addIncoming(n2, ma);
  for (size_t i = 0; i < cphi.size(); i++)
    cphi[i]->addIncoming(cur[i], ma);
  ab.CreateCondBr(ab.CreateICmpNE(n2, main), mh, rpre);

  // Remainder: the original loop, from iteration main, unless main == tc.
  IRBuilder<> rb(rpre);
  PHINode *rstart = rb.CreatePHI(ivStart->getType(), 2, "mj.ivstart");
  rstart->addIncoming(ivStart, xpre);
  Value *mm = nullptr, *allDone = nullptr;
  {
    IRBuilder<> tb(ma->getTerminator());
    mm = tb.CreateAdd(ivStart,
                      tb.CreateMul(ivStep, tb.CreateSExtOrTrunc(main, ivStep->getType())));
    allDone = tb.CreateICmpEQ(main, tc, "mj.all");
  }
  rstart->addIncoming(mm, ma);
  SmallVector<PHINode *, 4> rc;
  for (size_t i = 0; i < carried.size(); i++) {
    PHINode *p = rb.CreatePHI(carried[i]->getType(), 2, carried[i]->getName() + ".rest");
    p->addIncoming(carried[i]->getIncomingValueForBlock(xpre), xpre);
    p->addIncoming(cur[i], ma);
    rc.push_back(p);
  }
  PHINode *done = rb.CreatePHI(i1, 2, "mj.done");
  done->addIncoming(ConstantInt::getFalse(C), xpre);
  done->addIncoming(allDone, ma);
  rb.CreateCondBr(done, skip, xh);
  // X's header phis now come from rpre.
  for (PHINode &p : xh->phis()) {
    int idx = p.getBasicBlockIndex(xpre);
    p.setIncomingBlock(idx, rpre);
    if (&p == iv) {
      p.setIncomingValue(idx, rstart);
      continue;
    }
    for (size_t i = 0; i < carried.size(); i++)
      if (carried[i] == &p)
        p.setIncomingValue(idx, rc[i]);
  }
  // Skipping the original loop: X's exit gets the carried values.
  IRBuilder<> sb(skip);
  sb.CreateBr(xexit);
  for (PHINode &p : xexit->phis()) {
    Value *v = p.getIncomingValueForBlock(xl);
    Value *r = nullptr;
    for (size_t i = 0; i < carried.size(); i++)
      if (carried[i]->getIncomingValueForBlock(xl) == v)
        r = rc[i];
    p.addIncoming(r ? r : PoisonValue::get(p.getType()), skip);
  }
  return true;
}


// True when every instruction of W has a vector form (Lanes lanes).
static bool widenableInst(Instruction &I) {
  auto scalarTy = [](Type *t) { return t->isIntegerTy() || t->isFloatingPointTy(); };
  if (isa<PHINode>(I) || I.isTerminator())
    return scalarTy(I.getType()) || I.isTerminator();
  if (!scalarTy(I.getType()))
    return false;
  if (isa<BinaryOperator>(I) || isa<UnaryOperator>(I) || isa<CmpInst>(I) || isa<SelectInst>(I))
    return true;
  if (auto *c = dyn_cast<CastInst>(&I))
    return scalarTy(c->getSrcTy());
  if (auto *ii = dyn_cast<IntrinsicInst>(&I)) {
    switch (ii->getIntrinsicID()) {
    case Intrinsic::fma: case Intrinsic::fmuladd: case Intrinsic::fabs:
    case Intrinsic::sqrt: case Intrinsic::minnum: case Intrinsic::maxnum:
    case Intrinsic::minimum: case Intrinsic::maximum: case Intrinsic::copysign:
    case Intrinsic::floor: case Intrinsic::ceil: case Intrinsic::trunc:
    case Intrinsic::round: case Intrinsic::rint: case Intrinsic::nearbyint:
    case Intrinsic::smin: case Intrinsic::smax: case Intrinsic::umin: case Intrinsic::umax:
    case Intrinsic::fptosi_sat: case Intrinsic::fptoui_sat:
      return true;
    default:
      return false;
    }
  }
  return false;
}

bool MaskedJammer::widenable(ArrayRef<BasicBlock *> wchain, ArrayRef<PHINode *> wphis,
                             ArrayRef<PHINode *> ephis) {
  for (BasicBlock *bb : wchain)
    for (Instruction &I : *bb)
      if (!widenableInst(I))
        return false;
  for (PHINode *p : ephis)
    if (!p->getType()->isIntegerTy() && !p->getType()->isFloatingPointTy())
      return false;
  return true;
}

void MaskedJammer::buildScalarW(BasicBlock *mh, BasicBlock *mw, BasicBlock *ma,
                                ArrayRef<BasicBlock *> wchain, ArrayRef<PHINode *> wphis,
                                ArrayRef<PHINode *> ephis, BasicBlock *wpre, BasicBlock *wl,
                                BasicBlock *E, ValueToValueMapTy *lane,
                                SmallVectorImpl<SmallVector<Value *, 8>> &res) {
  LLVMContext &C = F.getContext();
  Type *i1 = Type::getInt1Ty(C);
  IRBuilder<> wb(mw);
  struct LaneW {
    SmallVector<PHINode *, 8> state; // per W header phi
    PHINode *active;
    SmallVector<PHINode *, 8> res;   // per E phi
  };
  SmallVector<LaneW, MaxLanes> lw(Lanes);
  for (unsigned u = 0; u < Lanes; u++) {
    for (PHINode *p : wphis) {
      PHINode *s = wb.CreatePHI(p->getType(), 2, p->getName() + ".l");
      Value *init = p->getIncomingValueForBlock(wpre);
      auto it = lane[u].find(init);
      s->addIncoming(it != lane[u].end() ? static_cast<Value *>(it->second) : init, mh);
      lw[u].state.push_back(s);
    }
    lw[u].active = wb.CreatePHI(i1, 2, "mj.active");
    lw[u].active->addIncoming(ConstantInt::getTrue(C), mh);
    for (PHINode *e : ephis) {
      PHINode *r = wb.CreatePHI(e->getType(), 2, e->getName() + ".res");
      // A real value, not poison: select(c, x, poison) may fold to x.
      r->addIncoming(Constant::getNullValue(e->getType()), mh);
      lw[u].res.push_back(r);
    }
  }
  SmallVector<Value *, MaxLanes> act(Lanes);
  for (unsigned u = 0; u < Lanes; u++) {
    ValueToValueMapTy &vm = lane[u];
    for (size_t i = 0; i < wphis.size(); i++)
      vm[wphis[i]] = lw[u].state[i];
    act[u] = lw[u].active;
    for (PHINode *r : lw[u].res)
      res[u].push_back(r);
    for (BasicBlock *bb : wchain) {
      for (Instruction &I : *bb) {
        if (isa<PHINode>(I) || I.isTerminator())
          continue;
        Instruction *c = I.clone();
        wb.Insert(c);
        RemapInstruction(c, vm, RF_IgnoreMissingLocals | RF_NoModuleLevelChanges);
        vm[&I] = c;
      }
      auto *br = cast<BranchInst>(bb->getTerminator());
      if (!br->isConditional())
        continue;
      // Exit taken from this block?
      Value *cond = br->getCondition();
      auto it = vm.find(cond);
      Value *cv = it != vm.end() ? static_cast<Value *>(it->second) : cond;
      Value *taken = br->getSuccessor(0) == E ? cv : wb.CreateNot(cv);
      // select, not and: a lane that left may compute poison here.
      Value *now = wb.CreateSelect(act[u], taken, ConstantInt::getFalse(C), "mj.exit");
      for (size_t i = 0; i < ephis.size(); i++) {
        Value *v = ephis[i]->getIncomingValueForBlock(bb);
        auto vit = vm.find(v);
        Value *mv = vit != vm.end() ? static_cast<Value *>(vit->second) : v;
        res[u][i] = wb.CreateSelect(now, mv, res[u][i]);
      }
      act[u] = wb.CreateSelect(now, ConstantInt::getFalse(C), act[u], "mj.active");
    }
  }
  Value *any = ConstantInt::getFalse(C);
  for (unsigned u = 0; u < Lanes; u++) {
    for (size_t i = 0; i < wphis.size(); i++) {
      Value *nv = wphis[i]->getIncomingValueForBlock(wl);
      auto it = lane[u].find(nv);
      Value *mv = it != lane[u].end() ? static_cast<Value *>(it->second) : nv;
      // A lane that left keeps computing (W has no effects and cannot trap);
      // only its recorded exit values matter. Freezing the state with a
      // select would put the exit test into the recurrence's critical path.
      lw[u].state[i]->addIncoming(mv, mw);
    }
    lw[u].active->addIncoming(act[u], mw);
    for (size_t i = 0; i < ephis.size(); i++)
      lw[u].res[i]->addIncoming(res[u][i], mw);
    any = wb.CreateSelect(act[u], ConstantInt::getTrue(C), any);
  }
  wb.CreateCondBr(any, mw, ma);
}

// The joint loop on <Lanes x T> values: lane u of every vector is the value
// lane u's scalar copy would have. Values W takes from Fore (one per lane)
// are gathered with insertelement in mh, values from outside X are splats.
void MaskedJammer::buildVectorW(BasicBlock *mh, BasicBlock *mw, BasicBlock *ma,
                                ArrayRef<BasicBlock *> wchain, ArrayRef<PHINode *> wphis,
                                ArrayRef<PHINode *> ephis, BasicBlock *wpre, BasicBlock *wl,
                                BasicBlock *E, ValueToValueMapTy *lane,
                                SmallVectorImpl<SmallVector<Value *, 8>> &res) {
  LLVMContext &C = F.getContext();
  Module &M = *F.getParent();
  auto vty = [this](Type *t) { return FixedVectorType::get(t, Lanes); };
  IRBuilder<> hb(mh->getTerminator());
  DenseMap<Value *, Value *> vm;     // W value -> vector
  DenseMap<Value *, Value *> outer;  // Fore or invariant value -> vector (built in mh)
  auto widenOuter = [&](Value *v) -> Value * {
    auto it = outer.find(v);
    if (it != outer.end())
      return it->second;
    Value *r;
    if (auto *c = dyn_cast<Constant>(v)) {
      r = ConstantVector::getSplat(ElementCount::getFixed(Lanes), c);
    } else if (lane[0].count(v)) {
      r = PoisonValue::get(vty(v->getType()));
      for (unsigned u = 0; u < Lanes; u++)
        r = hb.CreateInsertElement(r, static_cast<Value *>(lane[u][v]), hb.getInt64(u));
    } else {
      r = hb.CreateVectorSplat(Lanes, v);
    }
    outer[v] = r;
    return r;
  };
  auto W = [&](Value *v) -> Value * {
    auto it = vm.find(v);
    if (it != vm.end())
      return it->second;
    return widenOuter(v);
  };
  IRBuilder<> wb(mw);
  SmallVector<PHINode *, 8> state;
  for (PHINode *p : wphis) {
    PHINode *s = wb.CreatePHI(vty(p->getType()), 2, p->getName() + ".v");
    s->addIncoming(widenOuter(p->getIncomingValueForBlock(wpre)), mh);
    vm[p] = s;
    state.push_back(s);
  }
  Type *mty = vty(Type::getInt1Ty(C));
  PHINode *active = wb.CreatePHI(mty, 2, "mj.active");
  active->addIncoming(Constant::getAllOnesValue(mty), mh);
  SmallVector<PHINode *, 8> rphi;
  SmallVector<Value *, 8> rv;
  for (PHINode *e : ephis) {
    PHINode *r = wb.CreatePHI(vty(e->getType()), 2, e->getName() + ".res");
    r->addIncoming(Constant::getNullValue(vty(e->getType())), mh);
    rphi.push_back(r);
    rv.push_back(r);
  }
  Value *act = active;
  Constant *noLane = Constant::getNullValue(mty);
  for (BasicBlock *bb : wchain) {
    for (Instruction &I : *bb) {
      if (isa<PHINode>(I) || I.isTerminator())
        continue;
      Value *nv;
      if (auto *bo = dyn_cast<BinaryOperator>(&I)) {
        nv = wb.CreateBinOp(bo->getOpcode(), W(bo->getOperand(0)), W(bo->getOperand(1)));
        cast<Instruction>(nv)->copyIRFlags(bo);
      } else if (auto *uo = dyn_cast<UnaryOperator>(&I)) {
        nv = wb.CreateUnOp(uo->getOpcode(), W(uo->getOperand(0)));
        cast<Instruction>(nv)->copyIRFlags(uo);
      } else if (auto *ci = dyn_cast<CmpInst>(&I)) {
        nv = wb.CreateCmp(ci->getPredicate(), W(ci->getOperand(0)), W(ci->getOperand(1)));
        if (isa<FPMathOperator>(nv))
          cast<Instruction>(nv)->copyFastMathFlags(ci);
      } else if (auto *si = dyn_cast<SelectInst>(&I)) {
        nv = wb.CreateSelect(W(si->getCondition()), W(si->getTrueValue()),
                             W(si->getFalseValue()));
      } else if (auto *ca = dyn_cast<CastInst>(&I)) {
        nv = wb.CreateCast(ca->getOpcode(), W(ca->getOperand(0)), vty(ca->getType()));
        cast<Instruction>(nv)->copyIRFlags(ca);
      } else {
        auto *ii = cast<IntrinsicInst>(&I);
        SmallVector<Value *, 3> args;
        for (Value *a : ii->args())
          args.push_back(W(a));
        SmallVector<Type *, 2> over;
        Intrinsic::ID id = ii->getIntrinsicID();
        if (id == Intrinsic::fptosi_sat || id == Intrinsic::fptoui_sat)
          over = {vty(ii->getType()), vty(ii->getArgOperand(0)->getType())};
        else
          over = {vty(ii->getType())};
        Function *fn = Intrinsic::getOrInsertDeclaration(&M, id, over);
        nv = wb.CreateCall(fn, args);
        if (isa<FPMathOperator>(nv))
          cast<Instruction>(nv)->copyFastMathFlags(ii);
      }
      vm[&I] = nv;
    }
    auto *br = cast<BranchInst>(bb->getTerminator());
    if (!br->isConditional())
      continue;
    Value *cv = W(br->getCondition());
    Value *taken = br->getSuccessor(0) == E ? cv : wb.CreateNot(cv);
    Value *now = wb.CreateSelect(act, taken, noLane, "mj.exit");
    for (size_t i = 0; i < ephis.size(); i++)
      rv[i] = wb.CreateSelect(now, W(ephis[i]->getIncomingValueForBlock(bb)), rv[i]);
    act = wb.CreateSelect(now, noLane, act, "mj.active");
  }
  for (size_t i = 0; i < wphis.size(); i++)
    state[i]->addIncoming(W(wphis[i]->getIncomingValueForBlock(wl)), mw);
  active->addIncoming(act, mw);
  for (size_t i = 0; i < ephis.size(); i++)
    rphi[i]->addIncoming(rv[i], mw);
  Value *any = wb.CreateOrReduce(act);
  wb.CreateCondBr(any, mw, ma);
  // Per lane exit values for Aft.
  IRBuilder<> ab(ma);
  for (unsigned u = 0; u < Lanes; u++)
    for (size_t i = 0; i < ephis.size(); i++)
      res[u].push_back(ab.CreateExtractElement(rv[i], ab.getInt64(u)));
}

class MaskedJamPass : public PassInfoMixin<MaskedJamPass> {
public:
  explicit MaskedJamPass(Options o) : opts(o) {}
  PreservedAnalyses run(Function &F, FunctionAnalysisManager &FAM) {
    bool changed = false;
    SmallPtrSet<BasicBlock *, 8> done;
    // Candidates need a two level nest; merge the inner loop's exit blocks
    // (and other trivial blocks) first, so its exits share one block.
    {
      auto &LI = FAM.getResult<LoopAnalysis>(F);
      bool nest = false;
      for (Loop *L : LI.getLoopsInPreorder())
        if (L->getSubLoops().size() == 1 && L->getSubLoops()[0]->getSubLoops().empty())
          nest = true;
      if (!nest)
        return PreservedAnalyses::all();
      FunctionPassManager fpm;
      fpm.addPass(SimplifyCFGPass());
      fpm.addPass(LoopSimplifyPass());
      fpm.addPass(LCSSAPass());
      fpm.run(F, FAM);
      changed = true;
    }
    for (int round = 0; round < 8; round++) {
      auto &LI = FAM.getResult<LoopAnalysis>(F);
      auto &SE = FAM.getResult<ScalarEvolutionAnalysis>(F);
      MaskedJammer mj(F, LI, SE, opts);
      bool did = false;
      for (Loop *L : LI.getLoopsInPreorder()) {
        if (done.count(L->getHeader()))
          continue;
        done.insert(L->getHeader());
        if (mj.tryLoop(L)) {
          did = true;
          if (verifyFunction(F, &errs()))
            report_fatal_error("loops: masked jam produced invalid IR in " + F.getName());
          break;
        }
      }
      if (!did)
        break;
      changed = true;
      FAM.invalidate(F, PreservedAnalyses::none());
    }
    return changed ? PreservedAnalyses::none() : PreservedAnalyses::all();
  }

private:
  Options opts;
};

} // namespace

void addMaskedJam(FunctionPassManager &fpm, const Options &opts) {
  fpm.addPass(MaskedJamPass(opts));
}

std::string maskedJamOptions() {
  return std::to_string(MJLanes) + (MJVector ? "v" : "s");
}

} // namespace loops
} // namespace marmot

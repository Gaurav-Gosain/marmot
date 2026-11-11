// Versioning.cpp: bounds check versioning (Rule V in docs/loops.md).
//
// A loop L whose only exits besides the latch are checks that never fire
// when a precondition computed before L holds (bounds checks `i < len`, the
// rotated form `i != len`, slice checks `lo <= hi`) is versioned:
//
//     if (precondition)  fast copy of L, the check exits removed
//     else               L unchanged (panics exactly where Go panics)
//
// The precondition is built from the ranges ScalarEvolution gives the
// checked values over L's whole iteration space, plus bounds that rule out
// wrapping in the index arithmetic, so it implies every check passes in
// every iteration. The fast copy then has a single exit, which is what
// loop-parallelize and LLVM's vectorizer need.

#include "Internal.h"

#include "llvm/Analysis/AliasAnalysis.h"
#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/MemoryLocation.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/Instructions.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Transforms/Utils/BasicBlockUtils.h"
#include "llvm/Transforms/Utils/Cloning.h"
#include "llvm/Transforms/Utils/LoopUtils.h"
#include "llvm/Transforms/Utils/ScalarEvolutionExpander.h"

using namespace llvm;

namespace marmot {
namespace loops {
namespace {

// Magnitude limits that keep `invariant + sum of terms` from wrapping in 64
// bits: invariant parts in [-2^61, 2^61], each term step*j at most 2^60, at
// most MaxTerms terms.
constexpr int64_t InvariantLimit = int64_t(1) << 61;
constexpr int64_t TermLimit = int64_t(1) << 60;
constexpr unsigned MaxTerms = 4;

struct Cond {
  ICmpInst::Predicate pred; // signed or unsigned compare
  const SCEV *lhs, *rhs;
};

// A checked value loaded inside L (a row length len(b[k])): it takes the
// values at addresses base + start + step*j, j in [0, count]. A check loop
// before L computes their minimum and maximum.
struct LoadBound {
  LoadInst *ld;
  const SCEV *base, *start, *step, *count;
  Value *mn = nullptr, *mx = nullptr;
  Value *vbase = nullptr, *vstart = nullptr, *vstep = nullptr, *vcount = nullptr;
};

// A condition on a loaded value: (loadLeft ? bound pred other : other pred
// bound), where bound is the minimum or maximum of load.
struct LoadCond {
  ICmpInst::Predicate pred;
  const SCEV *other;
  int load;
  bool useMax, loadLeft;
  Value *vother = nullptr;
};

// One side of a compare: a range of an affine value, or a loaded value.
struct Side {
  const SCEV *mn = nullptr, *mx = nullptr;
  int load = -1;
};

class Versioner {
public:
  Versioner(Function &F, LoopInfo &LI, ScalarEvolution &SE, DominatorTree &DT, AAResults &AA)
      : F(F), LI(LI), SE(SE), DT(DT), AA(AA) {}
  bool run(Loop *L, std::string &why);

private:
  Function &F;
  LoopInfo &LI;
  ScalarEvolution &SE;
  DominatorTree &DT;
  AAResults &AA;
  std::string why;
  std::vector<Cond> conds;
  std::vector<LoadBound> loads;
  std::vector<LoadCond> loadConds;
  // Assumptions (no-wrap of an induction) under which a loop's exit count is
  // exact; checked at run time like conds.
  SmallVector<const SCEVPredicate *, 4> preds;

  // Exit count of L's latch, if needed under predicates (Go's `i <= n`
  // only terminates when i does not wrap, so its count needs one).
  const SCEV *latchCount(Loop *L) {
    BasicBlock *latch = L->getLoopLatch();
    const SCEV *c = SE.getExitCount(L, latch);
    if (!isa<SCEVCouldNotCompute>(c))
      return c;
    SmallVector<const SCEVPredicate *, 4> p;
    c = SE.getPredicatedExitCount(L, latch, &p);
    if (!isa<SCEVCouldNotCompute>(c))
      for (const SCEVPredicate *q : p)
        if (!is_contained(preds, q))
          preds.push_back(q);
    return c;
  }

  bool reject(const Twine &msg) {
    why = msg.str();
    return false;
  }
  // Range [min, max] of S over all iterations of L, with conditions that
  // make it exact (no wrap) appended to conds.
  bool range(Loop *L, const SCEV *S, const SCEV *&mn, const SCEV *&mx);
  bool checkCondition(Loop *L, BranchInst *br, BasicBlock *exit);
  bool side(Loop *L, const SCEV *S, Side &out);
  int loadBound(Loop *L, LoadInst *ld);
  Value *emitCheckLoops(BasicBlock *&at);
};

// Rule V, loaded bounds: ld runs in every iteration of every loop between it
// and L, reads one address per iteration of exactly one recurrence, and no
// store or call in L may write what it reads.
int Versioner::loadBound(Loop *L, LoadInst *ld) {
  if (!ld->isSimple() || !ld->getType()->isIntegerTy(64)) {
    reject("loaded bound is not a plain 64-bit load");
    return -1;
  }
  for (Loop *m = LI.getLoopFor(ld->getParent()); m; m = m->getParentLoop()) {
    if (!DT.dominates(ld->getParent(), m->getLoopLatch())) {
      reject("loaded bound is not read in every iteration");
      return -1;
    }
    if (m == L)
      break;
  }
  const SCEV *ps = SE.getSCEV(ld->getPointerOperand());
  const SCEV *base = SE.getPointerBase(ps);
  if (!isa<SCEVUnknown>(base) || !SE.isLoopInvariant(base, L)) {
    reject("loaded bound through a pointer computed in the loop");
    return -1;
  }
  const SCEV *off = SE.getMinusSCEV(ps, base);
  const SCEV *coef, *lo, *hi;
  SmallVector<Term, 2> terms;
  SmallVector<const SCEVPredicate *, 4> p;
  if (isa<SCEVCouldNotCompute>(off) ||
      !decomposeAffine(SE, off, L, coef, lo, hi, &terms, /*latchOnly=*/true, &p)) {
    reject("loaded bound address is not affine");
    return -1;
  }
  for (const SCEVPredicate *q : p)
    if (!is_contained(preds, q))
      preds.push_back(q);
  LoadBound lb{ld, base, lo, nullptr, nullptr};
  if (coef->isZero() && terms.empty()) {
    // One address for all of L (the load could not be hoisted because it
    // sits after a check): a check loop of one iteration.
    lb.step = SE.getZero(lo->getType());
    lb.count = SE.getZero(lo->getType());
  } else if (!coef->isZero() && terms.empty()) {
    lb.step = coef;
    lb.count = latchCount(L);
  } else if (coef->isZero() && terms.size() == 1 && lo == hi) {
    // (decomposeAffine widened hi by the inner range; the start is lo.)
    lb.step = terms[0].step;
    lb.count = terms[0].count;
  } else if (coef->isZero() && terms.size() == 1) {
    lb.step = terms[0].step;
    lb.count = terms[0].count;
    if (!SE.isKnownNonNegative(lb.step)) {
      reject("loaded bound address decreases");
      return -1;
    }
  } else {
    reject("loaded bound address depends on more than one induction");
    return -1;
  }
  if (isa<SCEVCouldNotCompute>(lb.count)) {
    reject("loaded bound trip count not computable");
    return -1;
  }
  lb.count = SE.getTruncateOrZeroExtend(lb.count, lo->getType());
  MemoryLocation loc = MemoryLocation::getBeforeOrAfter(ld->getPointerOperand(),
                                                        ld->getAAMetadata());
  for (BasicBlock *bb : L->blocks())
    for (Instruction &I : *bb) {
      if (!I.mayWriteToMemory())
        continue;
      if (isa<CallBase>(I) || !isa<StoreInst>(I) || isModSet(AA.getModRefInfo(&I, loc))) {
        reject("the loop may write a loaded bound");
        return -1;
      }
    }
  loads.push_back(lb);
  return int(loads.size() - 1);
}

bool Versioner::side(Loop *L, const SCEV *S, Side &out) {
  if (auto *u = dyn_cast<SCEVUnknown>(S))
    if (auto *ld = dyn_cast<LoadInst>(u->getValue()))
      if (L->contains(ld)) {
        out.load = loadBound(L, ld);
        return out.load >= 0;
      }
  return range(L, S, out.mn, out.mx);
}

bool Versioner::range(Loop *L, const SCEV *S, const SCEV *&mn, const SCEV *&mx) {
  if (SE.isLoopInvariant(S, L)) {
    mn = mx = S;
    return true;
  }
  Type *ty = S->getType();
  if (!ty->isIntegerTy(64))
    return reject("checked value is not a 64-bit integer");
  const SCEV *coef, *lo, *hi;
  SmallVector<Term, 4> terms;
  SmallVector<const SCEVPredicate *, 4> p;
  if (!decomposeAffine(SE, S, L, coef, lo, hi, &terms, /*latchOnly=*/true, &p))
    return reject("checked value is not affine in the loop");
  for (const SCEVPredicate *q : p)
    if (!is_contained(preds, q))
      preds.push_back(q);
  const SCEV *btc = latchCount(L);
  if (isa<SCEVCouldNotCompute>(btc))
    return reject("latch exit count not computable");
  btc = SE.getTruncateOrZeroExtend(btc, ty);
  if (!coef->isZero())
    terms.push_back({coef, btc});
  if (terms.size() > MaxTerms)
    return reject("too many recurrences in a checked value");
  // Each term step*j, j in [0, count]: |step| * count <= TermLimit.
  for (Term &t : terms) {
    const SCEV *count = SE.getTruncateOrZeroExtend(t.count, ty);
    auto *c = dyn_cast<SCEVConstant>(t.step);
    if (c && c->getAPInt().isZero())
      continue;
    if (c && c->getAPInt().abs().ule(TermLimit)) {
      uint64_t maxCount = uint64_t(TermLimit) / c->getAPInt().abs().getZExtValue();
      conds.push_back({ICmpInst::ICMP_ULE, count, SE.getConstant(ty, maxCount)});
      continue;
    }
    // Symbolic step (nsieve's j += i): |step| <= 2^30 and count <= 2^30.
    int64_t half = int64_t(1) << 30;
    conds.push_back({ICmpInst::ICMP_SLE, t.step, SE.getConstant(ty, half)});
    conds.push_back({ICmpInst::ICMP_SGE, t.step, SE.getConstant(ty, -half)});
    conds.push_back({ICmpInst::ICMP_ULE, count, SE.getConstant(ty, half)});
  }
  // The invariant parts lo and hi (lo == hi + inner ranges) stay small.
  for (const SCEV *v : {lo, hi}) {
    conds.push_back({ICmpInst::ICMP_SLE, v, SE.getConstant(ty, InvariantLimit)});
    conds.push_back({ICmpInst::ICMP_SGE, v, SE.getConstant(ty, -InvariantLimit)});
  }
  const SCEV *span = SE.getMulExpr(coef, btc);
  if (coef->isZero()) {
    mn = lo;
    mx = hi;
  } else if (SE.isKnownPositive(coef)) {
    mn = lo;
    mx = SE.getAddExpr(hi, span);
  } else if (SE.isKnownNegative(coef)) {
    mn = SE.getAddExpr(lo, span);
    mx = hi;
  } else {
    return reject("stride of unknown sign");
  }
  return true;
}

// br on an icmp whose exit edge goes to `exit`: add conditions under which
// the exit edge is never taken.
bool Versioner::checkCondition(Loop *L, BranchInst *br, BasicBlock *exit) {
  auto *cmp = dyn_cast<ICmpInst>(br->getCondition());
  if (!cmp)
    return reject("exit condition is not an integer compare");
  // Predicate that must hold to stay in the loop.
  ICmpInst::Predicate stay = cmp->getPredicate();
  if (br->getSuccessor(0) == exit)
    stay = ICmpInst::getInversePredicate(stay);
  Value *a = cmp->getOperand(0), *b = cmp->getOperand(1);
  if (!SE.isSCEVable(a->getType()))
    return reject("exit compare on a non-integer");
  // Nil check on an address inside an object (p := &s[i]): p != nil holds
  // in every iteration when the address, as an integer, stays above zero
  // over the whole range (with the usual no-wrap bounds).
  if (a->getType()->isPointerTy()) {
    if (isa<ConstantPointerNull>(a))
      std::swap(a, b);
    if (!isa<ConstantPointerNull>(b) || stay != ICmpInst::ICMP_NE)
      return reject("pointer compare other than a nil check");
    Type *ity = Type::getInt64Ty(F.getContext());
    const SCEV *pa = SE.getPtrToIntExpr(SE.getSCEV(a), ity);
    if (isa<SCEVCouldNotCompute>(pa))
      return reject("nil check on an address ScalarEvolution cannot follow");
    const SCEV *mn, *mx;
    if (!range(L, pa, mn, mx))
      return false;
    conds.push_back({ICmpInst::ICMP_SGT, mn, SE.getZero(ity)});
    return true;
  }
  const SCEV *sa = SE.getSCEV(a), *sb = SE.getSCEV(b);
  // Normalize to a stay predicate of the form x < y or x <= y (or x != y).
  switch (stay) {
  case ICmpInst::ICMP_UGT:
  case ICmpInst::ICMP_UGE:
  case ICmpInst::ICMP_SGT:
  case ICmpInst::ICMP_SGE:
    std::swap(sa, sb);
    stay = ICmpInst::getSwappedPredicate(stay);
    break;
  case ICmpInst::ICMP_NE: {
    // i != len: the affine side must stay below the other side (invariant,
    // or a loaded bound).
    auto isLoad = [&](const SCEV *x) {
      auto *u = dyn_cast<SCEVUnknown>(x);
      return u && isa<LoadInst>(u->getValue()) &&
             L->contains(cast<Instruction>(u->getValue()));
    };
    if (SE.isLoopInvariant(sa, L) || isLoad(sa))
      std::swap(sa, sb);
    if (!SE.isLoopInvariant(sb, L) && !isLoad(sb))
      return reject("x != y with both sides varying");
    break;
  }
  default:
    break;
  }
  bool isUnsigned = ICmpInst::isUnsigned(stay) || stay == ICmpInst::ICMP_NE;
  bool strict = stay == ICmpInst::ICMP_ULT || stay == ICmpInst::ICMP_SLT ||
                stay == ICmpInst::ICMP_NE;
  if (stay == ICmpInst::ICMP_EQ)
    return reject("exit unless equal");
  if (SE.isLoopInvariant(sa, L) && SE.isLoopInvariant(sb, L)) {
    conds.push_back({stay, sa, sb});
    return true;
  }
  Side A, B;
  if (!side(L, sa, A) || !side(L, sb, B))
    return false;
  if (A.load >= 0 && B.load >= 0)
    return reject("both sides of a check are loaded");
  // max(a) < min(b) (or <=), compared signed; for unsigned and != also
  // min(a) >= 0, so signed and unsigned agree.
  Type *ty = sa->getType();
  ICmpInst::Predicate lt = strict ? ICmpInst::ICMP_SLT : ICmpInst::ICMP_SLE;
  if (isUnsigned) {
    if (A.load >= 0)
      loadConds.push_back({ICmpInst::ICMP_SGE, SE.getZero(ty), A.load, false, true});
    else
      conds.push_back({ICmpInst::ICMP_SGE, A.mn, SE.getZero(ty)});
  }
  if (A.load >= 0)
    loadConds.push_back({lt, B.mn, A.load, true, true});
  else if (B.load >= 0)
    loadConds.push_back({lt, A.mx, B.load, false, false});
  else
    conds.push_back({lt, A.mx, B.mn});
  return true;
}

bool Versioner::run(Loop *L, std::string &whyOut) {
  auto done = [&](bool ok) {
    whyOut = why;
    return ok;
  };
  if (!L->isLoopSimplifyForm())
    return done(reject("not in simplified form"));
  BasicBlock *latch = L->getLoopLatch();
  auto *latchBr = dyn_cast<BranchInst>(latch->getTerminator());
  if (!latchBr || !latchBr->isConditional() || L->isLoopExiting(latch) == false)
    return done(reject("latch is not the loop's counted exit"));
  if (isa<SCEVCouldNotCompute>(latchCount(L)))
    return done(reject("latch exit count not computable"));
  BasicBlock *latchExit = nullptr;
  for (BasicBlock *s : successors(latch))
    if (!L->contains(s))
      latchExit = s;

  // Every other exiting branch of L (at any depth) is a check to version.
  SmallVector<std::pair<BranchInst *, BasicBlock *>, 8> checks;
  for (BasicBlock *bb : L->blocks()) {
    if (bb == latch)
      continue;
    for (BasicBlock *s : successors(bb)) {
      if (L->contains(s))
        continue;
      auto *br = dyn_cast<BranchInst>(bb->getTerminator());
      if (!br || !br->isConditional())
        return done(reject("exit through a non-branch"));
      if (s == latchExit)
        return done(reject("break to the loop's normal exit"));
      checks.push_back({br, s});
    }
  }
  if (checks.empty())
    return done(reject("no checks"));
  for (auto &c : checks)
    if (!checkCondition(L, c.first, c.second)) {
      std::string s;
      raw_string_ostream os(s);
      c.first->getCondition()->print(os);
      why += " [" + StringRef(s).trim().str() + "]";
      return done(false);
    }

  // Drop conditions ScalarEvolution proves; the rest must be expandable.
  BasicBlock *pre = L->getLoopPreheader();
  SCEVExpander exp(SE, "bcv");
  std::vector<Cond> runtime;
  for (Cond &c : conds) {
    if (SE.isKnownPredicate(c.pred, c.lhs, c.rhs))
      continue;
    if (SE.isKnownPredicate(ICmpInst::getInversePredicate(c.pred), c.lhs, c.rhs))
      return done(reject("a check always fails"));
    if (!exp.isSafeToExpandAt(c.lhs, pre->getTerminator()) ||
        !exp.isSafeToExpandAt(c.rhs, pre->getTerminator()))
      return done(reject("precondition not expandable before the loop"));
    runtime.push_back(c);
  }
  for (LoadBound &lb : loads)
    for (const SCEV *x : {lb.base, lb.start, lb.step, lb.count})
      if (!exp.isSafeToExpandAt(x, pre->getTerminator()))
        return done(reject("loaded bound range not expandable before the loop"));
  for (LoadCond &c : loadConds)
    if (!exp.isSafeToExpandAt(c.other, pre->getTerminator()))
      return done(reject("precondition not expandable before the loop"));

  formLCSSARecursively(*L, DT, &LI, &SE);

  // Expand every value the precondition needs before cloning anything: the
  // expander may put values of other loops into LCSSA form and rewrite their
  // uses, which must happen while the dominator tree is still exact (a clone
  // made first would see such a use rewritten to poison).
  Instruction *at = pre->getTerminator();
  IRBuilder<> b(at);
  Value *cond = b.getTrue();
  for (Cond &c : runtime) {
    Value *l = exp.expandCodeFor(c.lhs, nullptr, at);
    Value *r = exp.expandCodeFor(c.rhs, l->getType(), at);
    b.SetInsertPoint(at);
    cond = b.CreateAnd(cond, b.CreateICmp(c.pred, l, r), "bcv.ok");
  }
  for (const SCEVPredicate *p : preds) {
    Value *fails = exp.expandCodeForPredicate(p, at);
    b.SetInsertPoint(at);
    cond = b.CreateAnd(cond, b.CreateNot(fails), "bcv.ok");
  }
  Type *i64 = Type::getInt64Ty(F.getContext());
  for (LoadBound &lb : loads) {
    lb.vbase = exp.expandCodeFor(lb.base, nullptr, at);
    lb.vstart = exp.expandCodeFor(lb.start, i64, at);
    lb.vstep = exp.expandCodeFor(lb.step, i64, at);
    lb.vcount = exp.expandCodeFor(lb.count, i64, at);
  }
  for (LoadCond &c : loadConds)
    c.vother = exp.expandCodeFor(c.other, i64, at);
  if (std::getenv("MARMOT_LOOPS_DEBUG")) {
    errs() << "loops: before versioning\n";
    F.print(errs());
  }

  // Guard block: the old preheader. The slow loop gets a fresh preheader.
  BasicBlock *header = L->getHeader();
  BasicBlock *slowPre = SplitEdge(pre, header, &DT, &LI);
  slowPre->setName(header->getName() + ".slow.pre");
  ValueToValueMapTy vmap;
  SmallVector<BasicBlock *, 16> blocks;
  Loop *fast = cloneLoopWithPreheader(slowPre, pre, L, vmap, ".fast", &LI, &DT, blocks);
  remapInstructionsInBlocks(blocks, vmap);
  auto *fastPre = cast<BasicBlock>(vmap[slowPre]);

  // Exit phis of the normal exit get the fast loop's values.
  for (PHINode &phi : latchExit->phis()) {
    for (unsigned i = 0, e = phi.getNumIncomingValues(); i < e; i++) {
      BasicBlock *in = phi.getIncomingBlock(i);
      if (!L->contains(in))
        continue;
      Value *v = phi.getIncomingValue(i);
      auto it = vmap.find(v);
      phi.addIncoming(it != vmap.end() ? static_cast<Value *>(it->second) : v,
                      cast<BasicBlock>(vmap[in]));
    }
  }
  // In the fast copy the checks never fire: branch straight on.
  for (auto &c : checks) {
    auto *nbr = cast<BranchInst>(vmap[c.first]);
    BasicBlock *stay = nbr->getSuccessor(0) == c.second ? nbr->getSuccessor(1)
                                                         : nbr->getSuccessor(0);
    BranchInst::Create(stay, nbr->getIterator());
    nbr->eraseFromParent();
  }

  // The guard: pre now ends in "br slowPre".
  Instruction *preBr = pre->getTerminator();
  if (loads.empty()) {
    BranchInst::Create(fastPre, slowPre, cond, preBr->getIterator());
    preBr->eraseFromParent();
  } else {
    // Scalar conditions first: they keep the check loops' reads in bounds.
    BasicBlock *checks0 = BasicBlock::Create(F.getContext(), "bcv.loads", &F, slowPre);
    BranchInst::Create(checks0, slowPre, cond, preBr->getIterator());
    preBr->eraseFromParent();
    BasicBlock *cur = checks0;
    Value *c2 = emitCheckLoops(cur);
    IRBuilder<> eb(cur);
    eb.CreateCondBr(c2, fastPre, slowPre);
  }
  (void)fast;
  whyOut = std::to_string(checks.size()) + " checks, " +
           std::to_string(runtime.size() + preds.size() + loadConds.size()) +
           " run time conditions, " + std::to_string(loads.size()) + " check loops";
  return true;
}

// Emits one loop per loaded bound, starting in block `at` (which it leaves
// pointing at the block after the last loop), and returns the conjunction of
// the load conditions. Their inputs were expanded in the guard block.
Value *Versioner::emitCheckLoops(BasicBlock *&at) {
  LLVMContext &C = F.getContext();
  Type *i64 = Type::getInt64Ty(C);
  Type *i8 = Type::getInt8Ty(C);
  for (LoadBound &lb : loads) {
    Value *base = lb.vbase, *start = lb.vstart, *step = lb.vstep, *count = lb.vcount;
    BasicBlock *body = BasicBlock::Create(C, "bcv.load", &F, at->getNextNode());
    BasicBlock *after = BasicBlock::Create(C, "bcv.load.done", &F, body->getNextNode());
    IRBuilder<> b(at);
    b.CreateBr(body);
    IRBuilder<> lb2(body);
    PHINode *j = lb2.CreatePHI(i64, 2, "j");
    PHINode *mn = lb2.CreatePHI(i64, 2, "mn");
    PHINode *mx = lb2.CreatePHI(i64, 2, "mx");
    j->addIncoming(ConstantInt::get(i64, 0), at);
    mn->addIncoming(ConstantInt::get(i64, INT64_MAX), at);
    mx->addIncoming(ConstantInt::get(i64, INT64_MIN), at);
    Value *off = lb2.CreateAdd(start, lb2.CreateMul(step, j));
    Value *addr = lb2.CreateGEP(i8, base, off);
    LoadInst *v = lb2.CreateLoad(i64, addr, "bound");
    v->setAAMetadata(lb.ld->getAAMetadata());
    Value *mn2 = lb2.CreateBinaryIntrinsic(Intrinsic::smin, mn, v);
    Value *mx2 = lb2.CreateBinaryIntrinsic(Intrinsic::smax, mx, v);
    Value *j2 = lb2.CreateAdd(j, ConstantInt::get(i64, 1));
    j->addIncoming(j2, body);
    mn->addIncoming(mn2, body);
    mx->addIncoming(mx2, body);
    // j runs over [0, count]; count < 2^62 is implied by the slice sizes the
    // loop itself indexes, and j2 <= count is an unsigned test.
    BranchInst *back = lb2.CreateCondBr(lb2.CreateICmpULE(j2, count), body, after);
    // Not a loop for loop-parallelize to look at.
    back->setMetadata("marmot.par", MDNode::get(C, {}));
    IRBuilder<> ab(after);
    lb.mn = ab.CreatePHI(i64, 1, "bound.min");
    cast<PHINode>(lb.mn)->addIncoming(mn2, body);
    lb.mx = ab.CreatePHI(i64, 1, "bound.max");
    cast<PHINode>(lb.mx)->addIncoming(mx2, body);
    at = after;
  }
  IRBuilder<> b(at);
  Value *cond = b.getTrue();
  for (LoadCond &c : loadConds) {
    Value *other = c.vother;
    LoadBound &lb = loads[c.load];
    Value *bound = c.useMax ? lb.mx : lb.mn;
    b.SetInsertPoint(at);
    Value *t = c.loadLeft ? b.CreateICmp(c.pred, bound, other) : b.CreateICmp(c.pred, other, bound);
    cond = b.CreateAnd(cond, t, "bcv.ok");
  }
  return cond;
}

} // namespace

bool versionBoundsChecks(Function &F, Loop *L, LoopInfo &LI, ScalarEvolution &SE,
                         DominatorTree &DT, AAResults &AA, std::string &why) {
  Versioner v(F, LI, SE, DT, AA);
  return v.run(L, why);
}

} // namespace loops
} // namespace marmot

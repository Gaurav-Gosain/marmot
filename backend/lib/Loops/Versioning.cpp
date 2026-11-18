// Versioning.cpp: bounds check versioning (rule V).
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

// Whether leaving a loop through exit block s always ends in a panic: a short
// chain of blocks (dedicated exits, LCSSA blocks, a shared panic block) that
// reaches a call to a noreturn function or `unreachable`, with nothing that
// branches back to normal code.
bool leadsToPanic(BasicBlock *s) {
  for (unsigned steps = 0; s && steps < 8; steps++) {
    for (Instruction &I : *s)
      if (auto *cb = dyn_cast<CallBase>(&I); cb && cb->doesNotReturn())
        return true;
    if (isa<UnreachableInst>(s->getTerminator()))
      return true;
    s = s->getSingleSuccessor();
  }
  return false;
}

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
  // Header phis that change only on a growth path (append's cap and ptr):
  // when no growth happens they keep their start value.
  ValueToSCEVMapTy frozen;

  // True when s refers to an induction of a loop that does not contain L:
  // expanding it before L recomputes that loop's exit values from its
  // inductions, and LSR then keeps extra values live in that loop.
  bool foreign(Loop *L, const SCEV *s) {
    return SCEVExprContains(s, [&](const SCEV *x) {
      auto *ar = dyn_cast<SCEVAddRecExpr>(x);
      return ar && !ar->getLoop()->contains(L);
    });
  }

  // Rewrites foreign parts of a SCEV to IR values that already hold them, so
  // the expander does not rebuild another loop's exit values from its
  // inductions:
  // - a subexpression equal to the SCEV of a value defined outside L that L
  //   uses, or a header phi's incoming value from the preheader, becomes that
  //   value (each one dominates L's preheader);
  // - an induction of L whose start is foreign gets the start of a header phi
  //   with the same step (its preheader incoming value) plus an invariant offset.
  // The values stay the same, so the precondition is unchanged. The pinned
  // values dominate L's preheader only: a precondition expanded further out
  // (hoisted check loops) must not use them (expansion is at L's preheader).
  DenseMap<const SCEV *, Value *> pinVals;
  Loop *pinLoop = nullptr;
  void collectPinVals(Loop *L) {
    pinVals.clear();
    pinLoop = L;
    BasicBlock *pre = L->getLoopPreheader();
    auto add = [&](Value *v) {
      auto *I = dyn_cast<Instruction>(v);
      if (!I || L->contains(I) || !SE.isSCEVable(I->getType()) ||
          !DT.dominates(I, pre->getTerminator()))
        return;
      const SCEV *s = SE.getSCEV(I);
      // Closed forms too: an exit value of another loop (an LCSSA phi) has a
      // SCEV in terms of that loop's start values, which expanding would
      // keep live across that loop.
      if (!isa<SCEVUnknown>(s) && !isa<SCEVConstant>(s))
        pinVals.try_emplace(s, I);
    };
    for (PHINode &h : L->getHeader()->phis())
      add(h.getIncomingValueForBlock(pre));
    for (BasicBlock *bb : L->blocks())
      for (Instruction &I : *bb)
        for (Value *op : I.operands())
          add(op);
  }
  struct Pinner : SCEVRewriteVisitor<Pinner> {
    Versioner &V;
    Loop *L;
    Pinner(Versioner &v, Loop *l) : SCEVRewriteVisitor(v.SE), V(v), L(l) {}
    const SCEV *visit(const SCEV *s) {
      auto it = V.pinVals.find(s);
      if (it != V.pinVals.end())
        return V.SE.getUnknown(it->second);
      return SCEVRewriteVisitor::visit(s);
    }
    const SCEV *visitAddRecExpr(const SCEVAddRecExpr *e) {
      if (e->getLoop() != L || !e->isAffine() || !V.foreign(L, e->getStart()))
        return SCEVRewriteVisitor::visitAddRecExpr(e);
      // Prefer a header phi of L with the same step whose start differs by a
      // constant (pop's len-1 against its own len phi): its preheader value
      // holds exactly this side. Then a value that holds the start, then a
      // phi with an invariant offset.
      BasicBlock *pre = L->getLoopPreheader();
      const SCEV *step = e->getStepRecurrence(V.SE);
      const SCEV *fallback = nullptr;
      for (PHINode &h : L->getHeader()->phis()) {
        if (!V.SE.isSCEVable(h.getType()))
          continue;
        auto *ph = dyn_cast<SCEVAddRecExpr>(V.SE.getSCEV(&h));
        if (!ph || ph->getLoop() != L || !ph->isAffine() || ph->getStepRecurrence(V.SE) != step ||
            ph->getType() != e->getType())
          continue;
        const SCEV *off = V.SE.getMinusSCEV(e->getStart(), ph->getStart());
        const SCEV *start = V.SE.getAddExpr(V.SE.getUnknown(h.getIncomingValueForBlock(pre)), off);
        // Same values as e in every iteration: keep e's wrap flags.
        if (isa<SCEVConstant>(off))
          return V.SE.getAddRecExpr(start, step, L, e->getNoWrapFlags());
        if (!fallback && !V.foreign(L, off))
          fallback = V.SE.getAddRecExpr(start, step, L, e->getNoWrapFlags());
      }
      const SCEV *s0 = visit(e->getStart());
      if (!V.foreign(L, s0))
        return V.SE.getAddRecExpr(s0, step, L, e->getNoWrapFlags());
      if (fallback)
        return fallback;
      return SCEVRewriteVisitor::visitAddRecExpr(e);
    }
  };
  const SCEV *pin(Loop *L, const SCEV *s) {
    if (pinLoop != L)
      collectPinVals(L);
    return Pinner(*this, L).visit(s);
  }
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
  if (!ruleOn(Rule::BcvLoaded)) {
    reject(Twine("loaded bound (") + ruleFlag(Rule::BcvLoaded) + ")");
    return -1;
  }
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
    if (ruleOn(Rule::BcvPin) && !isa<SCEVCouldNotCompute>(lb.count))
      lb.count = pin(L, lb.count);
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
  if (ruleOn(Rule::BcvPin))
    btc = pin(L, btc);
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
  if (!frozen.empty()) {
    sa = SCEVParameterRewriter::rewrite(sa, SE, frozen);
    sb = SCEVParameterRewriter::rewrite(sb, SE, frozen);
  }
  if (ruleOn(Rule::BcvPin)) {
    sa = pin(L, sa);
    sb = pin(L, sb);
  }
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
  // x != 0 with x affine (pop's len != 0): min(x) > 0, as for a nil check
  // (RuleV.lean nil_sound). The general x != y form below needs x < y,
  // which never holds for y = 0.
  if (ruleOn(Rule::BcvStride) && stay == ICmpInst::ICMP_NE && sb->isZero() &&
      !SE.isLoopInvariant(sa, L)) {
    const SCEV *mn, *mx;
    if (!range(L, sa, mn, mx))
      return false;
    conds.push_back({ICmpInst::ICMP_SGT, mn, SE.getZero(sa->getType())});
    return true;
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
  // Both sides affine in L with the same step (s[len(s)-1] with a shrinking
  // length): a_j - b_j is the same in every iteration, so compare the
  // difference once. The term limits checked by side() keep a_j and b_j
  // from wrapping, so a_j < b_j iff 0 < b_0 - a_0 (RuleV.lean, equal steps).
  auto *ra = dyn_cast<SCEVAddRecExpr>(sa), *rb = dyn_cast<SCEVAddRecExpr>(sb);
  if (ruleOn(Rule::BcvStride) && A.load < 0 && B.load < 0 && ra && rb && ra->getLoop() == L &&
      rb->getLoop() == L && ra->isAffine() && rb->isAffine() &&
      ra->getStepRecurrence(SE) == rb->getStepRecurrence(SE)) {
    const SCEV *d = SE.getMinusSCEV(rb->getStart(), ra->getStart());
    if (SE.isLoopInvariant(d, L)) {
      conds.push_back({lt, SE.getZero(ty), d});
      return true;
    }
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
      // A check fails into a panic. Any other exit is the loop's own
      // condition (quicksort's `for a[i] < p`): versioning it would scan
      // the whole remaining range before every entry of a loop that
      // usually leaves early.
      if (!leadsToPanic(s))
        return done(reject("exit that does not panic (a data dependent loop condition)"));
      checks.push_back({br, s});
    }
  }
  // Growth branches (append): bb -> S -> M and bb -> M, S calls
  // runtime.growslice. Removing them in the fast copy is like removing a
  // check: the precondition proves the branch never goes to S.
  SmallVector<std::pair<BranchInst *, BasicBlock *>, 4> grows;
  for (BasicBlock *bb : L->blocks()) {
    auto *br = dyn_cast<BranchInst>(bb->getTerminator());
    if (!br || !br->isConditional() || bb == latch)
      continue;
    for (unsigned i = 0; i < 2; i++) {
      BasicBlock *S = br->getSuccessor(i), *M = br->getSuccessor(1 - i);
      if (!L->contains(S) || S->getSinglePredecessor() != bb || S->getSingleSuccessor() != M)
        continue;
      bool grows1 = false;
      for (Instruction &I : *S)
        if (auto *cb = dyn_cast<CallBase>(&I))
          if (Function *f = cb->getCalledFunction())
            grows1 |= f->getName() == "runtime.growslice";
      if (grows1)
        grows.push_back({br, S});
    }
  }
  if (!grows.empty() && !ruleOn(Rule::BcvGrowth))
    return done(reject(Twine("append growth branches (") + ruleFlag(Rule::BcvGrowth) + ")"));
  if (checks.empty() && grows.empty())
    return done(reject("no checks"));
  // Frozen phis: the latch value is a phi of a merge block whose incoming
  // values from outside the growth blocks are the header phi itself.
  for (PHINode &h : L->getHeader()->phis()) {
    auto *m = dyn_cast<PHINode>(h.getIncomingValueForBlock(latch));
    if (!m || grows.empty())
      continue;
    bool ok = true, viaGrow = false;
    for (unsigned i = 0; i < m->getNumIncomingValues(); i++) {
      bool fromGrow = any_of(grows, [&](auto &g) { return g.second == m->getIncomingBlock(i); });
      if (fromGrow)
        viaGrow = true;
      else if (m->getIncomingValue(i) != &h)
        ok = false;
    }
    if (ok && viaGrow && SE.isSCEVable(h.getType()))
      frozen[&h] = SE.getSCEV(h.getIncomingValueForBlock(L->getLoopPreheader()));
  }
  for (auto &g : grows)
    if (!checkCondition(L, g.first, g.second)) {
      why = "growth branch: " + why;
      return done(false);
    }
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

  // In the fast copy no growth happens: drop the growth blocks and fold the
  // merge phis they fed (append's ptr and cap become loop invariant).
  SmallVector<BasicBlock *, 4> deadGrow;
  SmallPtrSet<BasicBlock *, 4> merges;
  for (auto &g : grows) {
    auto *nbr = cast<BranchInst>(vmap[g.first]);
    auto *nS = cast<BasicBlock>(vmap[g.second]);
    BasicBlock *stay = nbr->getSuccessor(0) == nS ? nbr->getSuccessor(1) : nbr->getSuccessor(0);
    BranchInst::Create(stay, nbr->getIterator());
    nbr->eraseFromParent();
    deadGrow.push_back(nS);
    merges.insert(stay);
  }
  DeleteDeadBlocks(deadGrow);
  for (BasicBlock *m : merges)
    FoldSingleEntryPHINodes(m);
  if (!grows.empty())
    for (PHINode &h : make_early_inc_range(fast->getHeader()->phis()))
      if (Value *v = h.hasConstantValue()) {
        h.replaceAllUsesWith(v);
        h.eraseFromParent();
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
  whyOut = std::to_string(checks.size()) + " checks, " + std::to_string(grows.size()) +
           " growth branches, " +
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

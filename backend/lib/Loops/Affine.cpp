// Affine.cpp: splitting ScalarEvolution expressions into per-iteration
// ranges, shared by loop-parallelize and bounds check versioning.
#include "Internal.h"

#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"

using namespace llvm;

namespace marmot {
namespace loops {

// Upper bound on the backedge count of a loop inside L, for the range of
// its recurrences: the symbolic maximum over all exits, or (latchOnly) the
// latch exit's count, for the fast copy made by versioning where the latch
// is the only exit left.
static const SCEV *innerCount(ScalarEvolution &SE, const Loop *al, bool latchOnly,
                              SmallVectorImpl<const SCEVPredicate *> *preds) {
  if (!latchOnly)
    return SE.getSymbolicMaxBackedgeTakenCount(al);
  BasicBlock *latch = al->getLoopLatch();
  if (!latch || !al->isLoopExiting(latch))
    return SE.getCouldNotCompute();
  const SCEV *c = SE.getExitCount(al, latch);
  if (!isa<SCEVCouldNotCompute>(c) || !preds)
    return c;
  return SE.getPredicatedExitCount(al, latch, preds);
}

bool decomposeAffine(ScalarEvolution &SE, const SCEV *S, const Loop *L, const SCEV *&coef,
                     const SCEV *&lo, const SCEV *&hi, SmallVectorImpl<Term> *terms,
                     bool latchOnly, SmallVectorImpl<const SCEVPredicate *> *preds) {
  Type *ty = S->getType();
  if (SE.isLoopInvariant(S, L)) {
    coef = SE.getZero(ty);
    lo = hi = S;
    return true;
  }
  if (auto *ar = dyn_cast<SCEVAddRecExpr>(S)) {
    if (!ar->isAffine())
      return false;
    const Loop *al = ar->getLoop();
    const SCEV *step = ar->getStepRecurrence(SE);
    if (!SE.isLoopInvariant(step, L))
      return false;
    if (al == L) {
      if (!SE.isLoopInvariant(ar->getStart(), L))
        return false;
      coef = step;
      lo = hi = ar->getStart();
      return true;
    }
    if (!L->contains(al))
      return false;
    const SCEV *c, *l, *h;
    if (!decomposeAffine(SE, ar->getStart(), L, c, l, h, terms, latchOnly, preds))
      return false;
    const SCEV *maxBtc = innerCount(SE, al, latchOnly, preds);
    if (isa<SCEVCouldNotCompute>(maxBtc) || !SE.isLoopInvariant(maxBtc, L))
      return false;
    const SCEV *ext = SE.getMulExpr(step, SE.getTruncateOrZeroExtend(maxBtc, ty));
    if (SE.isKnownNonNegative(step))
      h = SE.getAddExpr(h, ext);
    else if (SE.isKnownNonPositive(step))
      l = SE.getAddExpr(l, ext);
    else
      return false;
    if (terms)
      terms->push_back({step, maxBtc});
    coef = c;
    lo = l;
    hi = h;
    return true;
  }
  if (auto *add = dyn_cast<SCEVAddExpr>(S)) {
    const SCEV *c = SE.getZero(ty), *l = SE.getZero(ty), *h = SE.getZero(ty);
    for (const SCEV *op : add->operands()) {
      const SCEV *oc, *ol, *oh;
      if (!decomposeAffine(SE, op, L, oc, ol, oh, terms, latchOnly, preds))
        return false;
      c = SE.getAddExpr(c, oc);
      l = SE.getAddExpr(l, ol);
      h = SE.getAddExpr(h, oh);
    }
    coef = c;
    lo = l;
    hi = h;
    return true;
  }
  if (auto *mul = dyn_cast<SCEVMulExpr>(S)) {
    if (mul->getNumOperands() != 2)
      return false;
    auto *k = dyn_cast<SCEVConstant>(mul->getOperand(0));
    if (!k)
      return false;
    size_t first = terms ? terms->size() : 0;
    const SCEV *oc, *ol, *oh;
    if (!decomposeAffine(SE, mul->getOperand(1), L, oc, ol, oh, terms, latchOnly, preds))
      return false;
    if (terms)
      for (size_t i = first; i < terms->size(); i++)
        (*terms)[i].step = SE.getMulExpr(k, (*terms)[i].step);
    coef = SE.getMulExpr(k, oc);
    if (k->getAPInt().isNonNegative()) {
      lo = SE.getMulExpr(k, ol);
      hi = SE.getMulExpr(k, oh);
    } else {
      lo = SE.getMulExpr(k, oh);
      hi = SE.getMulExpr(k, ol);
    }
    return true;
  }
  return false;
}

} // namespace loops
} // namespace marmot

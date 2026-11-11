// Parallelize.cpp: loop-parallelize.
//
// Finds counted loops whose iterations are independent (Rule P in
// docs/loops.md), outlines the loop into a chunk function
//
//     void F$parN(ctx *, int64 lo, int64 hi)   // runs iterations [lo, hi)
//
// and versions the loop in the caller:
//
//     if (run time checks pass && trip count is large enough)
//       rt_parallel_for(tripCount, grain, F$parN, ctx)
//     else
//       original loop
//
// Integer reductions (add, sub, xor, or, and, min, max) are split: each chunk
// starts from the identity and combines its partial result into a slot in
// ctx with one atomic read-modify-write. Wrapping integer arithmetic makes
// these operations associative and commutative, so the result is the same
// as the serial loop's.
//
// The pass runs at the start of the LLVM optimizer pipeline (after inlining
// and loop canonicalization, before the vectorizer), so the chunk function's
// loop is still vectorized by LLVM.

#include "marmot/Loops/Loops.h"
#include "Internal.h"

#include "llvm/Analysis/AliasAnalysis.h"
#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/MemoryLocation.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/IntrinsicInst.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/PassManager.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Transforms/Utils/Cloning.h"
#include "llvm/Transforms/Utils/LoopUtils.h"
#include "llvm/Transforms/Utils/ScalarEvolutionExpander.h"

#include <optional>

using namespace llvm;

namespace marmot {
namespace loops {
namespace {

// Cost units: roughly one simple instruction. A loop is run in parallel only
// when tripCount * costPerIteration reaches MinParallelWork, and each chunk
// gets at least GrainWork units.
constexpr uint64_t MinParallelWork = 200000;
constexpr uint64_t GrainWork = 20000;
// Assumed trip count of an inner loop whose trip count is not a constant.
constexpr uint64_t UnknownInnerTrips = 64;

enum class RedKind { Add, Xor, Or, And, SMin, SMax, UMin, UMax };

struct Reduction {
  PHINode *phi = nullptr;
  Value *next = nullptr; // the value carried around the back edge
  RedKind kind = RedKind::Add;
};

struct IndVar {
  PHINode *phi = nullptr;
  Value *start = nullptr;
  Value *step = nullptr;        // invariant in the loop
  Type *gepElemTy = nullptr;    // pointer induction: gep gepElemTy, phi, step
};

// The bytes an access touches in iteration k of the loop:
//   base + coef*k + [lo, hi] + [0, size)
struct Access {
  Instruction *inst = nullptr;
  bool store = false;
  bool affine = false; // false: base varies in the loop or the offset is not affine
  Value *ptr = nullptr;
  const SCEV *base = nullptr; // integer (ptrtoint) SCEV of the base pointer
  Value *baseValue = nullptr;
  const SCEV *coef = nullptr, *lo = nullptr, *hi = nullptr;
  uint64_t size = 0;
};

struct Plan {
  Loop *loop = nullptr;
  std::vector<IndVar> ivs;
  std::vector<Reduction> reds;
  std::vector<Value *> liveIns;
  // Run time conditions, all must hold: lhs <= rhs (signed).
  std::vector<std::pair<const SCEV *, const SCEV *>> leChecks;
  // Byte ranges [s1, e1) and [s2, e2) must not overlap (unsigned).
  struct Overlap {
    const SCEV *s1, *e1, *s2, *e2;
  };
  std::vector<Overlap> overlaps;
  uint64_t costPerIter = 1;
};

const char *kindName(RedKind k) {
  switch (k) {
  case RedKind::Add: return "add";
  case RedKind::Xor: return "xor";
  case RedKind::Or: return "or";
  case RedKind::And: return "and";
  case RedKind::SMin: return "smin";
  case RedKind::SMax: return "smax";
  case RedKind::UMin: return "umin";
  case RedKind::UMax: return "umax";
  }
  return "?";
}

Constant *identity(RedKind k, IntegerType *ty) {
  unsigned w = ty->getBitWidth();
  switch (k) {
  case RedKind::Add:
  case RedKind::Xor:
  case RedKind::Or:
  case RedKind::UMax:
    return ConstantInt::get(ty, 0);
  case RedKind::And:
  case RedKind::UMin:
    return ConstantInt::get(ty, APInt::getAllOnes(w));
  case RedKind::SMin:
    return ConstantInt::get(ty, APInt::getSignedMaxValue(w));
  case RedKind::SMax:
    return ConstantInt::get(ty, APInt::getSignedMinValue(w));
  }
  return nullptr;
}

AtomicRMWInst::BinOp rmwOp(RedKind k) {
  switch (k) {
  case RedKind::Add: return AtomicRMWInst::Add;
  case RedKind::Xor: return AtomicRMWInst::Xor;
  case RedKind::Or: return AtomicRMWInst::Or;
  case RedKind::And: return AtomicRMWInst::And;
  case RedKind::SMin: return AtomicRMWInst::Min;
  case RedKind::SMax: return AtomicRMWInst::Max;
  case RedKind::UMin: return AtomicRMWInst::UMin;
  case RedKind::UMax: return AtomicRMWInst::UMax;
  }
  return AtomicRMWInst::Add;
}

// The reduction operation of I, if I is one.
std::optional<RedKind> reductionOp(Instruction *I) {
  if (auto *bo = dyn_cast<BinaryOperator>(I)) {
    switch (bo->getOpcode()) {
    case Instruction::Add:
    case Instruction::Sub: return RedKind::Add;
    case Instruction::Xor: return RedKind::Xor;
    case Instruction::Or: return RedKind::Or;
    case Instruction::And: return RedKind::And;
    default: return std::nullopt;
    }
  }
  if (auto *ii = dyn_cast<IntrinsicInst>(I)) {
    switch (ii->getIntrinsicID()) {
    case Intrinsic::smin: return RedKind::SMin;
    case Intrinsic::smax: return RedKind::SMax;
    case Intrinsic::umin: return RedKind::UMin;
    case Intrinsic::umax: return RedKind::UMax;
    default: return std::nullopt;
    }
  }
  return std::nullopt;
}

class Parallelizer {
public:
  Parallelizer(Function &F, LoopInfo &LI, ScalarEvolution &SE, DominatorTree &DT,
               AAResults &AA, const Options &opts)
      : F(F), LI(LI), SE(SE), DT(DT), AA(AA), opts(opts), DL(F.getDataLayout()) {}

  // Tries L, then its children. Returns true after transforming one loop
  // (analyses are then stale).
  bool visit(Loop *L);

private:
  Function &F;
  LoopInfo &LI;
  ScalarEvolution &SE;
  DominatorTree &DT;
  AAResults &AA;
  const Options &opts;
  const DataLayout &DL;

  std::string why;
  bool reject(const Twine &msg) {
    why = msg.str();
    return false;
  }
  void remark(Loop *L, const Twine &msg) {
    if (!opts.remarks)
      return;
    errs() << "loops: " << F.getName() << ": loop " << L->getHeader()->getName()
           << " (depth " << L->getLoopDepth() << "): " << msg << "\n";
  }

  bool analyze(Loop *L, Plan &P);
  bool analyzeHeaderPhis(Loop *L, Plan &P);
  bool analyzeReduction(Loop *L, PHINode *phi, Reduction &R);
  bool analyzeBody(Loop *L, Plan &P, std::vector<Access> &accs);
  bool analyzeMemory(Loop *L, Plan &P, std::vector<Access> &accs);
  bool decompose(const SCEV *S, Loop *L, const SCEV *&coef, const SCEV *&lo,
                 const SCEV *&hi);
  bool liveOuts(Loop *L, Plan &P);
  uint64_t cost(Loop *L);
  void transform(Plan &P);
  Function *outline(Plan &P, StructType *ctxTy);
};

// Loops are marked on their latch terminator once decided, so that the
// restart after a transform does not look at them again.
constexpr const char *DoneMD = "marmot.par";

bool isDecided(Loop *L) {
  BasicBlock *latch = L->getLoopLatch();
  return latch && latch->getTerminator()->getMetadata(DoneMD);
}
void markDecided(Loop *L) {
  if (BasicBlock *latch = L->getLoopLatch())
    latch->getTerminator()->setMetadata(DoneMD,
                                        MDNode::get(latch->getContext(), {}));
}

bool Parallelizer::visit(Loop *L) {
  if (!isDecided(L)) {
    Plan P;
    P.loop = L;
    if (analyze(L, P)) {
      remark(L, "parallel, " + Twine(P.reds.size()) + " reductions, " +
                    Twine(P.leChecks.size() + P.overlaps.size()) +
                    " run time checks, cost/iter " + Twine(P.costPerIter));
      markDecided(L);
      transform(P);
      return true;
    }
    remark(L, "not parallel: " + why);
    markDecided(L);
  } else if (L->getLoopLatch() &&
             L->getLoopLatch()->getTerminator()->getMetadata("marmot.par.orig")) {
    // The serial fallback of a loop we already parallelized: leave its inner
    // loops alone.
    return false;
  }
  for (Loop *child : *L)
    if (visit(child))
      return true;
  return false;
}

bool Parallelizer::analyze(Loop *L, Plan &P) {
  if (!L->isLoopSimplifyForm())
    return reject("not in simplified form");
  BasicBlock *latch = L->getLoopLatch();
  if (L->getExitingBlock() != latch)
    return reject("more than one exit, or the exit is not at the latch "
                  "(bounds check or break in the loop)");
  if (!L->getExitBlock())
    return reject("no unique exit block");
  auto *br = dyn_cast<BranchInst>(latch->getTerminator());
  if (!br || !br->isConditional())
    return reject("latch does not end in a conditional branch");
  const SCEV *btc = SE.getBackedgeTakenCount(L);
  if (isa<SCEVCouldNotCompute>(btc))
    return reject("trip count not computable");
  if (!btc->getType()->isIntegerTy() || btc->getType()->getIntegerBitWidth() > 64)
    return reject("trip count type");
  for (Loop *sub : L->getLoopsInPreorder())
    if (sub != L && isa<SCEVCouldNotCompute>(SE.getSymbolicMaxBackedgeTakenCount(sub)))
      return reject("inner loop at " + sub->getHeader()->getName() +
                    " has no bound on its trip count");
  formLCSSARecursively(*L, DT, &LI, &SE);
  if (!analyzeHeaderPhis(L, P))
    return false;
  std::vector<Access> accs;
  if (!analyzeBody(L, P, accs))
    return false;
  if (!analyzeMemory(L, P, accs))
    return false;
  if (!liveOuts(L, P))
    return false;
  P.costPerIter = std::max<uint64_t>(1, cost(L));
  // Values used in the loop but defined outside it.
  SetVector<Value *> ins;
  for (BasicBlock *bb : L->blocks())
    for (Instruction &I : *bb)
      for (Value *op : I.operands()) {
        if (auto *oi = dyn_cast<Instruction>(op)) {
          if (!L->contains(oi))
            ins.insert(op);
        } else if (isa<Argument>(op)) {
          ins.insert(op);
        }
      }
  P.liveIns.assign(ins.begin(), ins.end());
  for (Value *v : P.liveIns)
    if (v->getType()->isTokenTy() || v->getType()->isMetadataTy())
      return reject("token or metadata live-in");
  // Run time checks must be expandable in the preheader.
  SCEVExpander exp(SE, "par");
  Instruction *at = L->getLoopPreheader()->getTerminator();
  auto ok = [&](const SCEV *s) { return exp.isSafeToExpandAt(s, at); };
  if (!ok(btc))
    return reject("trip count not expandable");
  for (auto &c : P.leChecks)
    if (!ok(c.first) || !ok(c.second))
      return reject("run time check not expandable");
  for (auto &o : P.overlaps)
    if (!ok(o.s1) || !ok(o.e1) || !ok(o.s2) || !ok(o.e2))
      return reject("run time alias check not expandable");
  return true;
}

bool Parallelizer::analyzeHeaderPhis(Loop *L, Plan &P) {
  BasicBlock *header = L->getHeader();
  BasicBlock *latch = L->getLoopLatch();
  for (PHINode &phi : header->phis()) {
    Value *next = phi.getIncomingValueForBlock(latch);
    Value *start = phi.getIncomingValueForBlock(L->getLoopPreheader());
    // Induction variable: next = phi + step or gep phi, step.
    if (auto *bo = dyn_cast<BinaryOperator>(next)) {
      if (bo->getOpcode() == Instruction::Add && L->contains(bo)) {
        Value *step = nullptr;
        if (bo->getOperand(0) == &phi)
          step = bo->getOperand(1);
        else if (bo->getOperand(1) == &phi)
          step = bo->getOperand(0);
        if (step && L->isLoopInvariant(step) && phi.getType()->isIntegerTy()) {
          // An add chain that is also a reduction (sum += c) is fine either way;
          // treat it as an induction only when the phi has no use outside.
          bool usedOutside = false;
          for (User *u : bo->users())
            if (auto *ui = dyn_cast<Instruction>(u); ui && !L->contains(ui))
              usedOutside = true;
          for (User *u : phi.users())
            if (auto *ui = dyn_cast<Instruction>(u); ui && !L->contains(ui))
              usedOutside = true;
          if (!usedOutside) {
            P.ivs.push_back({&phi, start, step, nullptr});
            continue;
          }
        }
      }
    }
    if (auto *gep = dyn_cast<GetElementPtrInst>(next)) {
      if (gep->getPointerOperand() == &phi && gep->getNumIndices() == 1 &&
          L->isLoopInvariant(gep->getOperand(1)) && L->contains(gep)) {
        bool usedOutside = false;
        for (User *u : phi.users())
          if (auto *ui = dyn_cast<Instruction>(u); ui && !L->contains(ui))
            usedOutside = true;
        if (!usedOutside) {
          P.ivs.push_back({&phi, start, gep->getOperand(1), gep->getSourceElementType()});
          continue;
        }
      }
    }
    Reduction R;
    if (analyzeReduction(L, &phi, R)) {
      P.reds.push_back(R);
      continue;
    }
    return reject("header phi " + phi.getName() + " is neither an induction nor a "
                  "supported reduction (" + why + ")");
  }
  return true;
}

// A reduction is a set of values M containing phi such that every member
// other than phi is either
//   - m = r op x with r in M, x not in M (for sub: r - x), or
//   - a merge: a phi whose incoming values are all in M, or a select whose
//     two values are in M and whose condition is not,
// every use of a member inside the loop is by a member, and every use
// outside the loop is an exit phi of the value carried around the back edge.
// All ops are the same associative and commutative integer operation, so
// the loop's final value is start op (op of all x the loop applied).
bool Parallelizer::analyzeReduction(Loop *L, PHINode *phi, Reduction &R) {
  auto *ity = dyn_cast<IntegerType>(phi->getType());
  if (!ity) {
    if (phi->getType()->isFloatingPointTy())
      return reject("floating point reduction (needs -ffast-fp)");
    return reject("not an integer");
  }
  unsigned w = ity->getBitWidth();
  if (w != 8 && w != 16 && w != 32 && w != 64)
    return reject("integer width " + Twine(w));
  R.phi = phi;
  R.next = phi->getIncomingValueForBlock(L->getLoopLatch());
  SmallPtrSet<Instruction *, 16> M;
  SmallVector<Instruction *, 16> work;
  M.insert(phi);
  work.push_back(phi);
  std::optional<RedKind> kind;
  while (!work.empty()) {
    Instruction *m = work.pop_back_val();
    for (User *u : m->users()) {
      auto *ui = dyn_cast<Instruction>(u);
      if (!ui)
        return reject("non-instruction user");
      if (!L->contains(ui)) {
        auto *ep = dyn_cast<PHINode>(ui);
        if (!ep || ep->getParent() != L->getExitBlock() || m != R.next)
          return reject("value used outside the loop other than the final result");
        continue;
      }
      if (M.count(ui))
        continue;
      if (isa<PHINode>(ui)) {
        // merge, checked below
      } else if (auto *sel = dyn_cast<SelectInst>(ui)) {
        if (sel->getCondition() == m)
          return reject("reduction value used as a condition");
      } else if (auto k = reductionOp(ui)) {
        if (kind && *kind != *k)
          return reject("mixed operations");
        kind = k;
      } else {
        return reject("used by " + Twine(ui->getOpcodeName()));
      }
      M.insert(ui);
      work.push_back(ui);
    }
  }
  if (!kind)
    return reject("no reduction operation");
  for (Instruction *m : M) {
    if (m == phi)
      continue;
    if (auto *p = dyn_cast<PHINode>(m)) {
      for (Value *v : p->incoming_values()) {
        auto *vi = dyn_cast<Instruction>(v);
        if (!vi || !M.count(vi))
          return reject("merge phi with a value from outside the reduction");
      }
    } else if (auto *sel = dyn_cast<SelectInst>(m)) {
      auto in = [&](Value *v) {
        auto *vi = dyn_cast<Instruction>(v);
        return vi && M.count(vi);
      };
      if (!in(sel->getTrueValue()) || !in(sel->getFalseValue()) ||
          in(sel->getCondition()))
        return reject("select mixes the reduction with other values");
    } else {
      unsigned n = 0;
      for (Value *op : m->operands())
        if (auto *oi = dyn_cast<Instruction>(op); oi && M.count(oi))
          n++;
      if (n != 1)
        return reject("operation with the reduction value on both sides");
      if (auto *bo = dyn_cast<BinaryOperator>(m); bo && bo->getOpcode() == Instruction::Sub) {
        auto *lhs = dyn_cast<Instruction>(bo->getOperand(0));
        if (!lhs || !M.count(lhs))
          return reject("x - r is not a reduction");
      }
    }
  }
  auto *nexti = dyn_cast<Instruction>(R.next);
  if (!nexti || !M.count(nexti))
    return reject("back edge value is not part of the reduction");
  R.kind = *kind;
  return true;
}

bool Parallelizer::analyzeBody(Loop *L, Plan &P, std::vector<Access> &accs) {
  for (BasicBlock *bb : L->blocks()) {
    for (Instruction &I : *bb) {
      if (auto *ld = dyn_cast<LoadInst>(&I)) {
        if (!ld->isSimple())
          return reject("atomic or volatile load");
        Access a;
        a.inst = ld;
        a.ptr = ld->getPointerOperand();
        TypeSize ts = DL.getTypeStoreSize(ld->getType());
        if (ts.isScalable())
          return reject("scalable load");
        a.size = ts.getFixedValue();
        accs.push_back(a);
        continue;
      }
      if (auto *st = dyn_cast<StoreInst>(&I)) {
        if (!st->isSimple())
          return reject("atomic or volatile store");
        Access a;
        a.inst = st;
        a.store = true;
        a.ptr = st->getPointerOperand();
        TypeSize ts = DL.getTypeStoreSize(st->getValueOperand()->getType());
        if (ts.isScalable())
          return reject("scalable store");
        a.size = ts.getFixedValue();
        accs.push_back(a);
        continue;
      }
      if (auto *cb = dyn_cast<CallBase>(&I)) {
        if (auto *ii = dyn_cast<IntrinsicInst>(cb)) {
          switch (ii->getIntrinsicID()) {
          case Intrinsic::assume:
          case Intrinsic::lifetime_start:
          case Intrinsic::lifetime_end:
          case Intrinsic::experimental_noalias_scope_decl:
          case Intrinsic::dbg_value:
          case Intrinsic::dbg_declare:
          case Intrinsic::dbg_label:
          case Intrinsic::pseudoprobe:
            continue;
          default:
            break;
          }
        }
        if (cb->doesNotAccessMemory() && cb->willReturn() && cb->doesNotThrow() &&
            !cb->isConvergent())
          continue;
        Function *callee = cb->getCalledFunction();
        return reject("call to " +
                      (callee ? callee->getName() : StringRef("an indirect function")));
      }
      if (I.mayReadOrWriteMemory())
        return reject(Twine("memory instruction ") + I.getOpcodeName());
      if (I.mayThrow())
        return reject("may throw");
      if (isa<AllocaInst>(I))
        return reject("alloca in the loop");
    }
  }
  return true;
}

// Splits S (an integer offset) into coef*k + [lo, hi] for iteration k of L.
// Inner loop recurrences contribute their whole range over the inner loop's
// iterations; that range must not depend on L's iteration.
bool Parallelizer::decompose(const SCEV *S, Loop *L, const SCEV *&coef,
                             const SCEV *&lo, const SCEV *&hi) {
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
    if (!decompose(ar->getStart(), L, c, l, h))
      return false;
    const SCEV *maxBtc = SE.getSymbolicMaxBackedgeTakenCount(al);
    if (isa<SCEVCouldNotCompute>(maxBtc) || !SE.isLoopInvariant(maxBtc, L))
      return false;
    const SCEV *ext = SE.getMulExpr(step, SE.getTruncateOrZeroExtend(maxBtc, ty));
    if (SE.isKnownNonNegative(step))
      h = SE.getAddExpr(h, ext);
    else if (SE.isKnownNonPositive(step))
      l = SE.getAddExpr(l, ext);
    else
      return false;
    coef = c;
    lo = l;
    hi = h;
    return true;
  }
  if (auto *add = dyn_cast<SCEVAddExpr>(S)) {
    const SCEV *c = SE.getZero(ty), *l = SE.getZero(ty), *h = SE.getZero(ty);
    for (const SCEV *op : add->operands()) {
      const SCEV *oc, *ol, *oh;
      if (!decompose(op, L, oc, ol, oh))
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
    const SCEV *oc, *ol, *oh;
    if (!decompose(mul->getOperand(1), L, oc, ol, oh))
      return false;
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

// Rule P, memory part: different iterations touch disjoint bytes of every
// object some iteration writes.
bool Parallelizer::analyzeMemory(Loop *L, Plan &P, std::vector<Access> &accs) {
  Type *ity = DL.getIntPtrType(F.getContext());
  for (Access &a : accs) {
    const SCEV *s = SE.getSCEV(a.ptr);
    const SCEV *base = SE.getPointerBase(s);
    auto *bu = dyn_cast<SCEVUnknown>(base);
    if (!bu || !SE.isLoopInvariant(base, L))
      continue; // not affine
    const SCEV *off = SE.getMinusSCEV(s, base);
    if (isa<SCEVCouldNotCompute>(off) || off->getType() != ity)
      continue;
    if (!decompose(off, L, a.coef, a.lo, a.hi))
      continue;
    a.affine = true;
    a.baseValue = bu->getValue();
    a.base = SE.getPtrToIntExpr(base, ity);
    if (isa<SCEVCouldNotCompute>(a.base))
      a.affine = false;
  }
  for (Access &a : accs)
    if (a.store && !a.affine)
      return reject("store address is not affine in the loop's induction "
                    "or its base pointer changes in the loop");

  // Same base: iteration windows of width |coef| must hold every access.
  for (size_t i = 0; i < accs.size(); i++) {
    Access &x = accs[i];
    if (!x.store)
      continue;
    for (size_t j = 0; j < accs.size(); j++) {
      Access &y = accs[j];
      if (!y.affine || y.baseValue != x.baseValue)
        continue;
      if (y.coef != x.coef)
        return reject("accesses to one object advance at different rates");
      const SCEV *c = x.coef;
      if (SE.isKnownNegative(c))
        c = SE.getNegativeSCEV(c);
      else if (!SE.isKnownPositive(c))
        return reject("store to the same location in every iteration, or "
                      "stride of unknown sign");
      // x.hi + x.size - y.lo <= c and y.hi + y.size - x.lo <= c
      const SCEV *xs = SE.getConstant(c->getType(), x.size);
      const SCEV *ys = SE.getConstant(c->getType(), y.size);
      const SCEV *e1 = SE.getMinusSCEV(SE.getAddExpr(x.hi, xs), y.lo);
      const SCEV *e2 = SE.getMinusSCEV(SE.getAddExpr(y.hi, ys), x.lo);
      for (const SCEV *e : {e1, e2}) {
        if (SE.isKnownPredicate(ICmpInst::ICMP_SLE, e, c))
          continue;
        if (auto *d = dyn_cast<SCEVConstant>(SE.getMinusSCEV(c, e)))
          if (d->getAPInt().isNegative())
            return reject("accesses of neighbouring iterations overlap");
        P.leChecks.push_back({e, c});
      }
    }
  }

  // Different bases: whole-loop byte ranges must not overlap, unless alias
  // analysis already says the objects differ.
  const SCEV *btc = SE.getTruncateOrZeroExtend(SE.getBackedgeTakenCount(L), ity);
  auto add3 = [&](const SCEV *a, const SCEV *b, const SCEV *c) {
    return SE.getAddExpr(SE.getAddExpr(a, b), c);
  };
  auto range = [&](Access &a) -> std::pair<const SCEV *, const SCEV *> {
    const SCEV *span = SE.getMulExpr(a.coef, btc);
    const SCEV *zero = SE.getZero(ity);
    const SCEV *size = SE.getConstant(ity, a.size);
    bool neg = SE.isKnownNegative(a.coef);
    const SCEV *s = add3(a.base, a.lo, neg ? span : zero);
    const SCEV *e = SE.getAddExpr(add3(a.base, a.hi, size), neg ? zero : span);
    return {s, e};
  };
  // Group ranges by base so each pair of objects gets one check.
  struct Group {
    Value *base;
    bool store = false;
    bool affine = true;
    const SCEV *s = nullptr, *e = nullptr;
    std::vector<Access *> accs;
  };
  std::vector<Group> groups;
  std::vector<Access *> variant;
  for (Access &a : accs) {
    if (!a.affine) {
      variant.push_back(&a);
      continue;
    }
    Group *g = nullptr;
    for (Group &gg : groups)
      if (gg.base == a.baseValue)
        g = &gg;
    if (!g) {
      groups.push_back({a.baseValue});
      g = &groups.back();
    }
    if (!a.coef || (!SE.isKnownNonNegative(a.coef) && !SE.isKnownNegative(a.coef)))
      return reject("stride of unknown sign");
    auto [s, e] = range(a);
    g->s = g->s ? SE.getUMinExpr(g->s, s) : s;
    g->e = g->e ? SE.getUMaxExpr(g->e, e) : e;
    g->store |= a.store;
    g->accs.push_back(&a);
  }
  for (size_t i = 0; i < groups.size(); i++) {
    if (!groups[i].store)
      continue;
    for (size_t j = 0; j < groups.size(); j++) {
      if (i == j || (groups[j].store && j < i))
        continue;
      if (AA.isNoAlias(MemoryLocation::getBeforeOrAfter(groups[i].base),
                       MemoryLocation::getBeforeOrAfter(groups[j].base)))
        continue;
      P.overlaps.push_back({groups[i].s, groups[i].e, groups[j].s, groups[j].e});
    }
    for (Access *v : variant) {
      if (AA.isNoAlias(MemoryLocation::getBeforeOrAfter(groups[i].base),
                       MemoryLocation::getBeforeOrAfter(v->ptr)))
        continue;
      return reject("load through a pointer computed in the loop may alias a "
                    "stored object");
    }
  }
  return true;
}

// Every value defined in the loop and used after it must be a reduction's
// final value (exit phis of other values were rejected with the header phis).
bool Parallelizer::liveOuts(Loop *L, Plan &P) {
  for (BasicBlock *bb : L->blocks())
    for (Instruction &I : *bb)
      for (User *u : I.users()) {
        auto *ui = dyn_cast<Instruction>(u);
        if (ui && L->contains(ui))
          continue;
        bool ok = false;
        for (Reduction &R : P.reds)
          if (R.next == &I)
            ok = true;
        if (!ok)
          return reject("value " + I.getName() + " is used after the loop");
      }
  return true;
}

uint64_t Parallelizer::cost(Loop *L) {
  uint64_t total = 0;
  for (BasicBlock *bb : L->blocks()) {
    uint64_t mult = 1;
    for (Loop *in = LI.getLoopFor(bb); in && in != L; in = in->getParentLoop()) {
      unsigned tc = SE.getSmallConstantMaxTripCount(in);
      mult = std::min<uint64_t>(mult * (tc ? tc : UnknownInnerTrips), 1ull << 40);
    }
    uint64_t c = 0;
    for (Instruction &I : *bb) {
      switch (I.getOpcode()) {
      case Instruction::PHI:
      case Instruction::Br:
        break;
      case Instruction::SDiv:
      case Instruction::UDiv:
      case Instruction::SRem:
      case Instruction::URem:
      case Instruction::FDiv:
      case Instruction::FRem:
        c += 8;
        break;
      case Instruction::Call:
        c += 10;
        break;
      default:
        c += 1;
      }
    }
    total = std::min<uint64_t>(total + c * mult, 1ull << 50);
  }
  return total;
}

Function *Parallelizer::outline(Plan &P, StructType *ctxTy) {
  Loop *L = P.loop;
  Module &M = *F.getParent();
  LLVMContext &C = F.getContext();
  Type *i64 = Type::getInt64Ty(C);
  Type *ptr = PointerType::getUnqual(C);
  auto *fty = FunctionType::get(Type::getVoidTy(C), {ptr, i64, i64}, false);
  Function *body = Function::Create(fty, GlobalValue::InternalLinkage,
                                    F.getName() + "$par", &M);
  body->setAttributes(AttributeList::get(C, F.getAttributes().getFnAttrs(), {}, {}));
  body->removeFnAttr(Attribute::AlwaysInline);
  body->addFnAttr(Attribute::NoUnwind);
  body->addFnAttr("marmot-par-body");
  Argument *ctx = body->getArg(0), *lo = body->getArg(1), *hi = body->getArg(2);
  ctx->setName("ctx");
  lo->setName("lo");
  hi->setName("hi");
  ctx->addAttr(Attribute::NoAlias);

  BasicBlock *entry = BasicBlock::Create(C, "entry", body);
  BasicBlock *exit = BasicBlock::Create(C, "exit", body);
  IRBuilder<> b(entry);
  ValueToValueMapTy vmap;
  for (unsigned i = 0; i < P.liveIns.size(); i++) {
    Value *v = P.liveIns[i];
    Value *addr = b.CreateStructGEP(ctxTy, ctx, i);
    vmap[v] = b.CreateLoad(v->getType(), addr, v->getName() + ".in");
  }
  vmap[L->getLoopPreheader()] = entry;
  vmap[L->getExitBlock()] = exit;
  SmallVector<BasicBlock *, 16> blocks;
  for (BasicBlock *bb : L->blocks()) {
    BasicBlock *nb = CloneBasicBlock(bb, vmap, ".par", body);
    vmap[bb] = nb;
    blocks.push_back(nb);
  }
  remapInstructionsInBlocks(blocks, vmap);

  // Starting values for iteration lo.
  auto *header = cast<BasicBlock>(vmap[L->getHeader()]);
  auto *latch = cast<BasicBlock>(vmap[L->getLoopLatch()]);
  auto mapped = [&](Value *v) -> Value * {
    auto it = vmap.find(v);
    return it == vmap.end() ? v : static_cast<Value *>(it->second);
  };
  for (IndVar &iv : P.ivs) {
    auto *nphi = cast<PHINode>(vmap[iv.phi]);
    Value *start = mapped(iv.start);
    Value *step = mapped(iv.step);
    Value *s;
    if (iv.gepElemTy) {
      Value *n = b.CreateMul(lo, b.CreateSExtOrTrunc(step, i64));
      s = b.CreateGEP(iv.gepElemTy, start, n, iv.phi->getName() + ".lo");
    } else {
      Value *k = b.CreateSExtOrTrunc(lo, step->getType());
      s = b.CreateAdd(start, b.CreateMul(step, k), iv.phi->getName() + ".lo");
    }
    nphi->setIncomingValueForBlock(entry, s);
  }
  for (Reduction &R : P.reds) {
    auto *nphi = cast<PHINode>(vmap[R.phi]);
    nphi->setIncomingValueForBlock(entry,
                                   identity(R.kind, cast<IntegerType>(R.phi->getType())));
  }
  // Partial results restart from the identity, so a no-wrap flag that held
  // for the serial sums may not hold for them.
  for (BasicBlock *bb : blocks)
    for (Instruction &I : *bb)
      if (isa<OverflowingBinaryOperator>(I) && !isa<GetElementPtrInst>(I)) {
        I.setHasNoSignedWrap(false);
        I.setHasNoUnsignedWrap(false);
      }
  b.CreateBr(header);

  // Iteration counter k in [lo, hi) replaces the original exit test.
  IRBuilder<> hb(header, header->getFirstNonPHIIt());
  PHINode *k = PHINode::Create(i64, 2, "k");
  k->insertBefore(header->begin());
  k->addIncoming(lo, entry);
  Instruction *term = latch->getTerminator();
  IRBuilder<> lb(term);
  Value *k1 = lb.CreateAdd(k, ConstantInt::get(i64, 1), "k.next");
  Value *more = lb.CreateICmpULT(k1, hi, "k.more");
  lb.CreateCondBr(more, header, exit);
  term->eraseFromParent();
  k->addIncoming(k1, latch);

  // Combine partial reductions.
  IRBuilder<> eb(exit);
  for (unsigned i = 0; i < P.reds.size(); i++) {
    Reduction &R = P.reds[i];
    Value *part = vmap[R.next];
    Value *slot = eb.CreateStructGEP(ctxTy, ctx, P.liveIns.size() + i);
    eb.CreateAtomicRMW(rmwOp(R.kind), slot, part, MaybeAlign(),
                       AtomicOrdering::Monotonic);
  }
  eb.CreateRetVoid();
  return body;
}

void Parallelizer::transform(Plan &P) {
  Loop *L = P.loop;
  LLVMContext &C = F.getContext();
  Module &M = *F.getParent();
  Type *i64 = Type::getInt64Ty(C);
  Type *ptr = PointerType::getUnqual(C);
  BasicBlock *pre = L->getLoopPreheader();
  BasicBlock *header = L->getHeader();
  BasicBlock *exitBB = L->getExitBlock();
  BasicBlock *latch = L->getLoopLatch();

  // Mark the serial version so its inner loops are left alone.
  latch->getTerminator()->setMetadata("marmot.par.orig", MDNode::get(C, {}));

  SmallVector<Type *, 16> fields;
  for (Value *v : P.liveIns)
    fields.push_back(v->getType());
  for (Reduction &R : P.reds)
    fields.push_back(R.phi->getType());
  StructType *ctxTy = StructType::get(C, fields);

  Function *body = outline(P, ctxTy);

  // Conditions, in the preheader.
  SCEVExpander exp(SE, "par");
  Instruction *at = pre->getTerminator();
  Value *btc = exp.expandCodeFor(SE.getBackedgeTakenCount(L), nullptr, at);
  IRBuilder<> b(at);
  Value *tc = b.CreateAdd(b.CreateZExtOrTrunc(btc, i64), ConstantInt::get(i64, 1), "par.tc");
  uint64_t minIters = std::max<uint64_t>(2, (MinParallelWork + P.costPerIter - 1) / P.costPerIter);
  Value *cond = b.CreateICmpUGE(tc, ConstantInt::get(i64, minIters), "par.big");
  for (auto &c : P.leChecks) {
    Value *l = exp.expandCodeFor(c.first, nullptr, at);
    Value *r = exp.expandCodeFor(c.second, l->getType(), at);
    b.SetInsertPoint(at);
    cond = b.CreateAnd(cond, b.CreateICmpSLE(l, r), "par.ok");
  }
  for (auto &o : P.overlaps) {
    Value *s1 = exp.expandCodeFor(o.s1, nullptr, at);
    Value *e1 = exp.expandCodeFor(o.e1, s1->getType(), at);
    Value *s2 = exp.expandCodeFor(o.s2, s1->getType(), at);
    Value *e2 = exp.expandCodeFor(o.e2, s1->getType(), at);
    b.SetInsertPoint(at);
    Value *disjoint = b.CreateOr(b.CreateICmpULE(e1, s2), b.CreateICmpULE(e2, s1));
    cond = b.CreateAnd(cond, disjoint, "par.noalias");
  }
  b.SetInsertPoint(at);
  uint64_t grainIters = std::max<uint64_t>(1, (GrainWork + P.costPerIter - 1) / P.costPerIter);
  Value *grain = b.CreateBinaryIntrinsic(Intrinsic::umax, ConstantInt::get(i64, grainIters),
                                         b.CreateLShr(tc, 6), nullptr, "par.grain");

  BasicBlock *par = BasicBlock::Create(C, "par.run", &F, header);
  b.CreateCondBr(cond, par, header);
  at->eraseFromParent();

  // The context lives in the caller's frame; rt_parallel_for returns after
  // every chunk is done.
  IRBuilder<> eb(&*F.getEntryBlock().getFirstInsertionPt());
  AllocaInst *ctx = eb.CreateAlloca(ctxTy, nullptr, "par.ctx");
  IRBuilder<> pb(par);
  for (unsigned i = 0; i < P.liveIns.size(); i++)
    pb.CreateStore(P.liveIns[i], pb.CreateStructGEP(ctxTy, ctx, i));
  for (unsigned i = 0; i < P.reds.size(); i++)
    pb.CreateStore(P.reds[i].phi->getIncomingValueForBlock(pre),
                   pb.CreateStructGEP(ctxTy, ctx, P.liveIns.size() + i));
  FunctionCallee rpf = M.getOrInsertFunction(
      "rt_parallel_for", FunctionType::get(Type::getVoidTy(C), {i64, i64, ptr, ptr}, false));
  pb.CreateCall(rpf, {tc, grain, body, ctx});
  SmallVector<Value *, 4> results;
  for (unsigned i = 0; i < P.reds.size(); i++)
    results.push_back(pb.CreateLoad(P.reds[i].phi->getType(),
                                    pb.CreateStructGEP(ctxTy, ctx, P.liveIns.size() + i),
                                    P.reds[i].phi->getName() + ".par"));
  pb.CreateBr(exitBB);
  for (PHINode &phi : exitBB->phis()) {
    Value *v = phi.getIncomingValueForBlock(latch);
    Value *r = nullptr;
    for (unsigned i = 0; i < P.reds.size(); i++)
      if (P.reds[i].next == v)
        r = results[i];
    phi.addIncoming(r ? r : PoisonValue::get(phi.getType()), par);
  }
}

class ParallelizePass : public PassInfoMixin<ParallelizePass> {
public:
  explicit ParallelizePass(Options o) : opts(o) {}
  PreservedAnalyses run(Module &M, ModuleAnalysisManager &MAM) {
    auto &FAM = MAM.getResult<FunctionAnalysisManagerModuleProxy>(M).getManager();
    std::vector<Function *> fns;
    for (Function &F : M)
      if (!F.isDeclaration() && !F.hasAvailableExternallyLinkage() &&
          !F.hasOptNone() && !F.hasFnAttribute("marmot-par-body"))
        fns.push_back(&F);
    bool changed = false;
    for (Function *F : fns) {
      for (int round = 0; round < 64; round++) {
        auto &LI = FAM.getResult<LoopAnalysis>(*F);
        auto &SE = FAM.getResult<ScalarEvolutionAnalysis>(*F);
        auto &DT = FAM.getResult<DominatorTreeAnalysis>(*F);
        auto &AA = FAM.getResult<AAManager>(*F);
        Parallelizer par(*F, LI, SE, DT, AA, opts);
        bool did = false;
        for (Loop *L : LI)
          if (par.visit(L)) {
            did = true;
            break;
          }
        // LCSSA formation may have changed the function even without a transform.
        FAM.invalidate(*F, PreservedAnalyses::none());
        if (!did)
          break;
        changed = true;
      }
    }
    // Drop our markers.
    for (Function *F : fns)
      for (BasicBlock &bb : *F)
        if (Instruction *t = bb.getTerminator()) {
          t->setMetadata(DoneMD, nullptr);
          t->setMetadata("marmot.par.orig", nullptr);
        }
    return changed ? PreservedAnalyses::none() : PreservedAnalyses::all();
  }

private:
  Options opts;
};

} // namespace

void addParallelize(ModulePassManager &mpm, const Options &opts) {
  mpm.addPass(ParallelizePass(opts));
}

} // namespace loops
} // namespace marmot

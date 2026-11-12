// Parallelize.cpp: loop-parallelize.
//
// Finds counted loops whose iterations are independent (rule P), outlines the loop into a chunk function
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
#include "llvm/Analysis/AssumptionCache.h"
#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/MemoryLocation.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"
#include "llvm/Analysis/ValueTracking.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/Verifier.h"
#include "llvm/Support/ErrorHandling.h"
#include "llvm/IR/IntrinsicInst.h"
#include "llvm/IR/MDBuilder.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/PassManager.h"
#include "llvm/Support/CommandLine.h"
#include "llvm/Support/FileSystem.h"
#include <mutex>
#include "llvm/Support/raw_ostream.h"
#include "llvm/Transforms/Scalar/LICM.h"
#include "llvm/Transforms/Scalar/LoopPassManager.h"
#include "llvm/Transforms/Utils/Cloning.h"
#include "llvm/Transforms/Utils/LoopSimplify.h"
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
// Innermost loops are vectorized and run several units per nanosecond: a
// 16K element saxpy (cost 17/iteration, 278k units) takes about 4 us, less
// than starting the helpers is worth (rtcore: under ~10 us stays serial).
constexpr uint64_t MinParallelWorkInner = 2000000;
constexpr uint64_t GrainWorkInner = 200000;
} // namespace
// Testing aid: run every accepted loop in parallel, however small, so test
// suites with small loops exercise the parallel path (chunks of 1 iteration).
static cl::opt<bool> ForcePar("loops-force-par", cl::Hidden,
                              cl::desc("Loop optimizer: parallelize accepted loops of any size"));
namespace {
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
  // Polynomial induction {c0, +, c1, +, c2} (value c0 + c1*k + c2*k(k-1)/2):
  // the coefficients, expanded in the preheader and passed as live-ins.
  const SCEVAddRecExpr *poly = nullptr;
  SmallVector<Value *, 3> coef;
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
  int family = -1; // row access: index into Plan::families
  int priv = -1;   // access to a privatized reduction array (Plan::privs)
  const SCEV *stride = nullptr; // strided form: bytes per inner step t
};

// Rows reached through one row pointer load `p = load(table + slot)`, with
// slot = start + step*j for j in [0, count] (one row per iteration of L when
// perIter). Accesses through p touch [p + extLo, p + extHi).
struct Family {
  LoadInst *rowLoad = nullptr;
  const SCEV *slot = nullptr; // SCEV of the row pointer's address (the key)
  Value *table = nullptr;
  const SCEV *start = nullptr, *step = nullptr, *count = nullptr;
  const SCEV *extLo = nullptr, *extHi = nullptr;
  bool written = false, perIter = false;
  std::vector<Instruction *> insts;
};

// One entry of the run time disjointness test: a family's rows, or an
// object's whole-loop byte range [s, e).
struct RangeSource {
  int family = -1;
  const SCEV *s = nullptr, *e = nullptr;
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
  // Memory access classes for alias scopes in the chunk function: accesses
  // in different classes never overlap when one of the classes is written
  // (proved by alias analysis or by the run time checks above).
  struct AccessClass {
    std::vector<Instruction *> insts;
    bool store = false;
  };
  std::vector<AccessClass> classes;
  std::vector<Family> families;
  // Sources that rt_ranges_disjoint must find pairwise disjoint.
  std::vector<RangeSource> disjoint;
  // GPU offer: every access is in one of these whole-loop
  // byte ranges [s, e); flags 1 read, 2 written. Empty when not offered.
  struct GpuRange {
    const SCEV *s, *e;
    int64_t flags;
  };
  std::vector<GpuRange> gpuRanges;
  bool gpuOK = false;
  // Reduction arrays (hist[b]++): an alloca or global of `bytes` bytes
  // whose only accesses in L are `x = x + v` on elements of type elem. Each
  // chunk works on a zeroed private copy and adds it into the object at the
  // end with atomic adds.
  struct Private {
    Value *object;
    uint64_t bytes;
    IntegerType *elem;
  };
  std::vector<Private> privs;
  // Values of L used after it that depend only on (affine) inductions and
  // invariants: recomputed for the last iteration after the parallel run.
  std::vector<Instruction *> liveOutVals;
  // Certificate for verify (-loops-report): the accesses and, for every
  // (store, other access) pair Rule P checked, why they cannot conflict.
  std::vector<Access> accesses;
  struct Pair {
    int a, b;
    const char *why;
  };
  std::vector<Pair> pairs;
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
  bool extraExits = false; // analyze rejected L only for its extra exits
  // Analysis of the fast copy Rule V would make: exits other than the
  // latch's are ignored (they would be gone).
  bool ignoreExits = false;
  // Set when the memory analysis failed only because a pointer is loaded
  // inside L; in the fast copy LICM may hoist it, so the probe accepts.
  bool softMemReject = false;
  BasicBlock *latchExit = nullptr;
  const SCEV *tripBtc = nullptr;
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
  bool rowAccess(Loop *L, Plan &P, Access &a);
  void privatize(Loop *L, Plan &P, std::vector<Access> &accs);
  bool interleaved(Loop *L, Plan &P, Access &x, Access &y);
  bool decompose(const SCEV *S, Loop *L, const SCEV *&coef, const SCEV *&lo,
                 const SCEV *&hi);
  bool liveOuts(Loop *L, Plan &P);
  bool recomputable(Loop *L, Plan &P, Instruction *I);
  uint64_t cost(Loop *L);
  void transform(Plan &P);
  void writeReport(Plan &P);
  Value *emitRowCheck(Plan &P, SCEVExpander &exp, Instruction *at);
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

static cl::opt<std::string> ReportFile(
    "loops-report", cl::Hidden,
    cl::desc("Loop optimizer: append one JSON certificate per parallelized loop to this file"));

static std::string scevJSON(const SCEV *S) {
  if (!S)
    return "null";
  if (auto *c = dyn_cast<SCEVConstant>(S))
    if (c->getAPInt().getSignificantBits() <= 64)
      return std::to_string(c->getAPInt().getSExtValue());
  return "\"sym\"";
}

static std::string jsonStr(StringRef s) {
  std::string o = "\"";
  for (char ch : s) {
    if (ch == '"' || ch == '\\')
      o += '\\';
    if (uint8_t(ch) < 0x20)
      continue;
    o += ch;
  }
  return o + "\"";
}

void Parallelizer::writeReport(Plan &P) {
  if (ReportFile.empty())
    return;
  std::string j;
  raw_string_ostream os(j);
  Loop *L = P.loop;
  const SCEV *btc = tripBtc;
  // Loops often have unnamed headers: name them by their preorder position.
  std::string lname = L->getHeader()->getName().str();
  if (lname.empty()) {
    unsigned idx = 0;
    for (Loop *x : LI.getLoopsInPreorder()) {
      if (x == L)
        break;
      idx++;
    }
    lname = "loop" + std::to_string(idx) + "@depth" + std::to_string(L->getLoopDepth());
  }
  auto constDiff = [&](const SCEV *a, const SCEV *b) -> std::string {
    if (!a || !b || a->getType() != b->getType())
      return "null";
    if (auto *c = dyn_cast<SCEVConstant>(SE.getMinusSCEV(a, b)))
      if (c->getAPInt().getSignificantBits() <= 64)
        return std::to_string(c->getAPInt().getSExtValue());
    return "null";
  };
  os << "{\"func\":" << jsonStr(F.getName()) << ",\"loop\":" << jsonStr(lname)
     << ",\"btc\":" << (isa_and_nonnull<SCEVConstant>(btc) ? scevJSON(btc) : "null") << ",\"phis\":[";
  bool first = true;
  for (IndVar &iv : P.ivs) {
    os << (first ? "" : ",") << "{\"kind\":\"" << (iv.poly ? "poly" : "ind") << "\",\"width\":"
       << iv.phi->getType()->getPrimitiveSizeInBits().getFixedValue() << "}";
    first = false;
  }
  for (Reduction &R : P.reds) {
    os << (first ? "" : ",") << "{\"kind\":\"red\",\"op\":\"" << kindName(R.kind)
       << "\",\"width\":" << R.phi->getType()->getIntegerBitWidth() << "}";
    first = false;
  }
  os << "],\"accesses\":[";
  for (size_t i = 0; i < P.accesses.size(); i++) {
    Access &a = P.accesses[i];
    Value *base = a.baseValue ? a.baseValue : getUnderlyingObject(a.ptr);
    std::string bn = base && base->hasName() ? base->getName().str() : "";
    if (bn.empty() && base) {
      raw_string_ostream bs(bn);
      base->printAsOperand(bs, false);
    }
    os << (i ? "," : "") << "{\"id\":" << i << ",\"base\":" << jsonStr(bn)
       << ",\"store\":" << (a.store ? "true" : "false") << ",\"size\":" << a.size
       << ",\"coef\":" << scevJSON(a.coef) << ",\"lo\":" << scevJSON(a.lo)
       << ",\"hi\":" << scevJSON(a.hi) << ",\"span\":" << constDiff(a.hi, a.lo)
       << ",\"row\":" << (a.family >= 0 ? "true" : "false");
    if (a.stride)
      os << ",\"stride\":" << scevJSON(a.stride) << ",\"count\":" << scevJSON(tripBtc);
    os << "}";
  }
  os << "],\"pairs\":[";
  for (size_t i = 0; i < P.pairs.size(); i++) {
    Access &x = P.accesses[P.pairs[i].a], &y = P.accesses[P.pairs[i].b];
    os << (i ? "," : "") << "{\"a\":" << P.pairs[i].a << ",\"b\":" << P.pairs[i].b
       << ",\"why\":\"" << P.pairs[i].why << "\"";
    // Offsets of b relative to a's lo, when both are on one base and constant.
    if (x.baseValue && x.baseValue == y.baseValue)
      os << ",\"dlo\":" << constDiff(y.lo, x.lo) << ",\"dhi\":" << constDiff(y.hi, x.lo);
    os << "}";
  }
  os << "]}\n";
  static std::mutex mu; // batch mode compiles modules on several threads
  std::lock_guard<std::mutex> lock(mu);
  std::error_code ec;
  raw_fd_ostream out(ReportFile, ec, sys::fs::OF_Append);
  if (!ec)
    out << j;
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
      writeReport(P);
      transform(P);
      return true;
    }
    // Loops that append (a growslice call on a rare branch) are versioned
    // without the probe: the fast copy is a plain store loop that LLVM
    // vectorizes even when Rule P does not take it.
    bool appends = false;
    for (BasicBlock *bb : L->blocks())
      for (Instruction &I : *bb)
        if (auto *cb = dyn_cast<CallBase>(&I))
          if (Function *f = cb->getCalledFunction())
            appends |= f->getName() == "runtime.growslice";
    if (appends && opts.versioning && L->getSubLoops().empty()) {
      extraExits = false;
      std::string vwhy;
      if (versionBoundsChecks(F, L, LI, SE, DT, AA, vwhy)) {
        remark(L, "append versioned (" + vwhy + "); fast copy next");
        markDecided(L);
        L->getLoopLatch()->getTerminator()->setMetadata("marmot.par.orig",
                                                         MDNode::get(F.getContext(), {}));
        return true;
      }
      remark(L, "not parallel: " + why + "; append not versioned: " + vwhy);
      markDecided(L);
      return false;
    }
    if (extraExits && opts.versioning) {
      extraExits = false;
      std::string pwhy = why;
      // Version only when the fast copy would run in parallel: a
      // precondition that buys nothing costs time on every entry (fannkuch's
      // 2 to 3 iteration flip loop).
      Plan probe;
      probe.loop = L;
      ignoreExits = true;
      softMemReject = false;
      bool would = analyze(L, probe) || softMemReject;
      ignoreExits = false;
      if (!would) {
        remark(L, "not parallel: " + pwhy + "; not versioned: the fast copy would not be "
                  "parallel either (" + why + ")");
        markDecided(L);
        for (Loop *child : *L)
          if (visit(child))
            return true;
        return false;
      }
      std::string vwhy;
      if (versionBoundsChecks(F, L, LI, SE, DT, AA, vwhy)) {
        if (std::getenv("MARMOT_LOOPS_DEBUG")) {
          F.print(errs());
          if (verifyFunction(F, &errs()))
            errs() << "loops: verify failed after versioning\n";
        }
        remark(L, "bounds checks versioned (" + vwhy + "); fast copy next");
        markDecided(L);
        L->getLoopLatch()->getTerminator()->setMetadata("marmot.par.orig",
                                                         MDNode::get(F.getContext(), {}));
        return true;
      }
      remark(L, "not parallel: " + why + "; not versioned: " + vwhy);
    } else {
      remark(L, "not parallel: " + why);
    }
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
  if (!ignoreExits && L->getExitingBlock() != latch) {
    extraExits = true;
    return reject("more than one exit, or the exit is not at the latch "
                  "(bounds check or break in the loop)");
  }
  if (!ignoreExits && !L->getExitBlock())
    return reject("no unique exit block");
  auto *br = dyn_cast<BranchInst>(latch->getTerminator());
  if (!br || !br->isConditional())
    return reject("latch does not end in a conditional branch");
  latchExit = L->contains(br->getSuccessor(0)) ? br->getSuccessor(1) : br->getSuccessor(0);
  if (L->contains(latchExit))
    return reject("latch does not exit");
  const SCEV *btc = ignoreExits ? SE.getExitCount(L, latch) : SE.getBackedgeTakenCount(L);
  tripBtc = btc;
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
  for (IndVar &iv : P.ivs)
    if (iv.poly)
      for (const SCEV *op : iv.poly->operands())
        if (!ok(op))
          return reject("induction coefficients not expandable");
  for (auto &c : P.leChecks)
    if (!ok(c.first) || !ok(c.second))
      return reject("run time check not expandable");
  for (auto &g : P.gpuRanges)
    if (!ok(g.s) || !ok(g.e))
      P.gpuOK = false;
  for (RangeSource &r : P.disjoint) {
    if (r.family < 0) {
      if (!ok(r.s) || !ok(r.e))
        return reject("run time alias check not expandable");
      continue;
    }
    Family &f = P.families[r.family];
    for (const SCEV *x : {f.start, f.step, f.count, f.extLo, f.extHi})
      if (!ok(x))
        return reject("row check not expandable");
  }
  for (auto &o : P.overlaps)
    if (!ok(o.s1) || !ok(o.e1) || !ok(o.s2) || !ok(o.e2))
      return reject("run time alias check not expandable");
  // Rules not yet signed off (Rules.cpp).
  if ((!P.overlaps.empty() || !P.families.empty() || !P.disjoint.empty()) &&
      !ruleOn(Rule::RtChecks))
    return reject(Twine("needs run time alias or row checks (") + ruleFlag(Rule::RtChecks) + ")");
  if (!P.privs.empty() && !ruleOn(Rule::Privatize))
    return reject(Twine("needs privatized reduction arrays (") + ruleFlag(Rule::Privatize) + ")");
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
          // Uses after the loop are recomputed for the last iteration
          // (liveOuts).
          P.ivs.push_back({&phi, start, step, nullptr});
          continue;
        }
      }
    }
    if (auto *gep = dyn_cast<GetElementPtrInst>(next)) {
      if (gep->getPointerOperand() == &phi && gep->getNumIndices() == 1 &&
          L->isLoopInvariant(gep->getOperand(1)) && L->contains(gep)) {
        P.ivs.push_back({&phi, start, gep->getOperand(1), gep->getSourceElementType()});
        continue;
      }
    }
    // Any other integer recurrence of degree at most 2 in L, like the
    // (i+j)*(i+j+1)/2 that strength reduction leaves in spectralnorm.
    if (phi.getType()->isIntegerTy() && SE.isSCEVable(phi.getType())) {
      auto *ar = dyn_cast<SCEVAddRecExpr>(SE.getSCEV(&phi));
      bool usedOutside = false;
      for (User *u : phi.users())
        if (auto *ui = dyn_cast<Instruction>(u); ui && !L->contains(ui))
          usedOutside = true;
      if (ar && ar->getLoop() == L && ar->getNumOperands() <= 3 && !usedOutside &&
          all_of(ar->operands(), [&](const SCEV *op) { return SE.isLoopInvariant(op, L); })) {
        IndVar iv;
        iv.phi = &phi;
        iv.poly = ar;
        P.ivs.push_back(iv);
        continue;
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
        if (ignoreExits && ui->getParent() != latchExit)
          continue; // on a check exit path that the fast copy will not have
        if (!ep || ep->getParent() != latchExit || m != R.next)
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

bool Parallelizer::decompose(const SCEV *S, Loop *L, const SCEV *&coef, const SCEV *&lo,
                             const SCEV *&hi) {
  // In the probe for versioning, inner loops still have their check exits;
  // their latch counts are what the fast copy will run.
  SmallVector<const SCEVPredicate *, 2> preds;
  if (!decomposeAffine(SE, S, L, coef, lo, hi, nullptr, ignoreExits, ignoreExits ? &preds : nullptr))
    return false;
  return preds.empty();
}

// Rule P, memory part: different iterations touch disjoint bytes of every
// object some iteration writes.
// A load or store through a row pointer loaded in L (c[i][j] with c a
// [][]T): records it in a Family.
bool Parallelizer::rowAccess(Loop *L, Plan &P, Access &a) {
  Type *ity = DL.getIntPtrType(F.getContext());
  const SCEV *s = SE.getSCEV(a.ptr);
  auto *bu = dyn_cast<SCEVUnknown>(SE.getPointerBase(s));
  if (!bu)
    return false;
  auto *rowLd = dyn_cast<LoadInst>(bu->getValue());
  if (!rowLd || !L->contains(rowLd) || !rowLd->isSimple() || !rowLd->getType()->isPointerTy())
    return false;
  // The row pointer is read in every iteration of the loops around it.
  for (Loop *m = LI.getLoopFor(rowLd->getParent()); m; m = m->getParentLoop()) {
    if (!DT.dominates(rowLd->getParent(), m->getLoopLatch()))
      return false;
    if (m == L)
      break;
  }
  const SCEV *slot = SE.getSCEV(rowLd->getPointerOperand());
  // Offset of the access inside its row, over all of L.
  const SCEV *off = SE.getMinusSCEV(s, bu);
  const SCEV *oc, *ol, *oh;
  if (isa<SCEVCouldNotCompute>(off) || off->getType() != ity || !decompose(off, L, oc, ol, oh))
    return false;
  const SCEV *btc = SE.getTruncateOrZeroExtend(tripBtc, ity);
  const SCEV *span = SE.getMulExpr(oc, btc);
  const SCEV *zero = SE.getZero(ity);
  const SCEV *lo, *hi;
  if (oc->isZero() || SE.isKnownNonNegative(oc)) {
    lo = ol;
    hi = SE.getAddExpr(SE.getAddExpr(oh, span), SE.getConstant(ity, a.size));
  } else if (SE.isKnownNegative(oc)) {
    lo = SE.getAddExpr(ol, span);
    hi = SE.getAddExpr(oh, SE.getConstant(ity, a.size));
  } else {
    return false;
  }
  (void)zero;
  int fi = -1;
  for (size_t i = 0; i < P.families.size(); i++)
    if (P.families[i].slot == slot)
      fi = int(i);
  if (fi < 0) {
    Family f;
    f.rowLoad = rowLd;
    f.slot = slot;
    const SCEV *tb = SE.getPointerBase(slot);
    auto *tu = dyn_cast<SCEVUnknown>(tb);
    if (!tu || !SE.isLoopInvariant(tb, L))
      return false;
    f.table = tu->getValue();
    const SCEV *soff = SE.getMinusSCEV(slot, tb);
    const SCEV *sc, *sl, *sh;
    SmallVector<Term, 2> terms;
    if (isa<SCEVCouldNotCompute>(soff) ||
        !decomposeAffine(SE, soff, L, sc, sl, sh, &terms))
      return false;
    f.start = sl;
    if (!sc->isZero() && terms.empty()) {
      f.step = sc;
      f.count = btc;
      f.perIter = true;
    } else if (sc->isZero() && terms.size() == 1 && SE.isKnownNonNegative(terms[0].step)) {
      f.step = terms[0].step;
      f.count = SE.getTruncateOrZeroExtend(terms[0].count, ity);
    } else if (sc->isZero() && terms.empty()) {
      f.step = SE.getZero(ity);
      f.count = SE.getZero(ity);
    } else {
      return false;
    }
    // Nothing in L writes the row pointers.
    MemoryLocation loc =
        MemoryLocation::getBeforeOrAfter(rowLd->getPointerOperand(), rowLd->getAAMetadata());
    for (BasicBlock *bb : L->blocks())
      for (Instruction &I : *bb)
        if (I.mayWriteToMemory() && isModSet(AA.getModRefInfo(&I, loc)))
          return false;
    f.extLo = lo;
    f.extHi = hi;
    P.families.push_back(f);
    fi = int(P.families.size() - 1);
  } else {
    Family &f = P.families[fi];
    f.extLo = SE.getSMinExpr(f.extLo, lo);
    f.extHi = SE.getSMaxExpr(f.extHi, hi);
  }
  P.families[fi].written |= a.store;
  P.families[fi].insts.push_back(a.inst);
  a.family = fi;
  return true;
}

// Rule P condition 5, strided form: x and y are base + c*k + s + t*j with
// one inner recurrence j (step t, the same for both). All of L's iterations
// fit in one stride of j when W = c*btc + |sx - sy| + size <= t, so two
// accesses can only meet for the same j; there they meet only for the same k
// when sx == sy and c >= size. (transpose: b[j*n+i] over i.)
bool Parallelizer::interleaved(Loop *L, Plan &P, Access &x, Access &y) {
  auto parts = [&](Access &a, const SCEV *&start, const SCEV *&step) {
    const SCEV *off = SE.getMinusSCEV(SE.getSCEV(a.ptr), SE.getPointerBase(SE.getSCEV(a.ptr)));
    const SCEV *c, *lo, *hi;
    SmallVector<Term, 2> terms;
    if (isa<SCEVCouldNotCompute>(off) ||
        !decomposeAffine(SE, off, L, c, lo, hi, &terms, ignoreExits) || terms.size() != 1 ||
        !SE.isKnownPositive(terms[0].step))
      return false;
    start = lo; // the inner term only widens hi
    step = terms[0].step;
    return true;
  };
  const SCEV *sx, *tx, *sy, *ty;
  if (!parts(x, sx, tx) || !parts(y, sy, ty) || tx != ty)
    return false;
  if (sx != sy || !SE.isKnownPositive(x.coef))
    return false;
  uint64_t size = std::max(x.size, y.size);
  if (auto *cc = dyn_cast<SCEVConstant>(x.coef))
    if (cc->getAPInt().ult(size))
      return false;
  Type *ty2 = x.coef->getType();
  const SCEV *W = SE.getAddExpr(SE.getMulExpr(x.coef, SE.getTruncateOrZeroExtend(tripBtc, ty2)),
                                SE.getConstant(ty2, size));
  x.stride = y.stride = tx;
  if (SE.isKnownPredicate(ICmpInst::ICMP_SLE, W, tx))
    return true;
  if (auto *d = dyn_cast<SCEVConstant>(SE.getMinusSCEV(tx, W)))
    if (d->getAPInt().isNegative())
      return false;
  P.leChecks.push_back({W, tx});
  return true;
}

// Rule P condition 5, reduction arrays: an alloca or global (at most
// MaxPrivateBytes) whose every access in L is a load/store pair on one
// address computing x + v (or x - v) on one integer type, at offsets that are
// multiples of its size, and that alias analysis separates from every other
// access in L. Integer addition wraps and commutes, so chunks may count into
// private zeroed copies and add them into the object in any order.
constexpr uint64_t MaxPrivateBytes = 16384;

void Parallelizer::privatize(Loop *L, Plan &P, std::vector<Access> &accs) {
  for (Access &st : accs) {
    if (!st.store || st.affine || st.priv >= 0)
      continue;
    auto *si = cast<StoreInst>(st.inst);
    Value *obj = getUnderlyingObject(si->getPointerOperand());
    uint64_t bytes = 0;
    if (auto *ai = dyn_cast<AllocaInst>(obj)) {
      std::optional<TypeSize> sz = ai->getAllocationSize(DL);
      if (!ai->isStaticAlloca() || !sz || sz->isScalable())
        continue;
      bytes = sz->getFixedValue();
    } else if (auto *gv = dyn_cast<GlobalVariable>(obj)) {
      if (gv->isConstant() || !gv->hasInitializer())
        continue;
      bytes = DL.getTypeAllocSize(gv->getValueType());
    } else {
      continue;
    }
    if (bytes == 0 || bytes > MaxPrivateBytes || L->contains(dyn_cast<Instruction>(obj)))
      continue;
    // Every access to obj in L: a pair x = load p; store p, x +/- v.
    auto *ity = dyn_cast<IntegerType>(si->getValueOperand()->getType());
    if (!ity || bytes % (ity->getBitWidth() / 8) != 0)
      continue;
    uint64_t esz = ity->getBitWidth() / 8;
    bool ok = true;
    SmallVector<Access *, 4> mine;
    for (Access &a : accs) {
      if (getUnderlyingObject(a.ptr) != obj)
        continue;
      mine.push_back(&a);
      if (a.inst->getType() != ity && (!a.store || cast<StoreInst>(a.inst)->getValueOperand()->getType() != ity))
        ok = false;
      if (a.store) {
        auto *s2 = cast<StoreInst>(a.inst);
        Value *v = s2->getValueOperand();
        auto *bo = dyn_cast<BinaryOperator>(v);
        LoadInst *ld = nullptr;
        if (bo && (bo->getOpcode() == Instruction::Add || bo->getOpcode() == Instruction::Sub)) {
          ld = dyn_cast<LoadInst>(bo->getOperand(0));
          if (!ld && bo->getOpcode() == Instruction::Add)
            ld = dyn_cast<LoadInst>(bo->getOperand(1));
        }
        if (!ld || ld->getPointerOperand() != s2->getPointerOperand() || !ld->isSimple() ||
            !ld->hasOneUse())
          ok = false;
      } else {
        // The load must feed exactly the store's update.
        auto *ld = cast<LoadInst>(a.inst);
        bool feeds = ld->hasOneUse();
        if (feeds) {
          auto *bo = dyn_cast<BinaryOperator>(*ld->user_begin());
          feeds = bo && bo->hasOneUse() && isa<StoreInst>(*bo->user_begin()) &&
                  cast<StoreInst>(*bo->user_begin())->getPointerOperand() == ld->getPointerOperand();
        }
        ok &= feeds;
      }
      // Element aligned offsets.
      const SCEV *off = SE.getMinusSCEV(SE.getSCEV(a.ptr), SE.getSCEV(obj));
      if (isa<SCEVCouldNotCompute>(off) || SE.getConstantMultiple(off).urem(esz) != 0)
        ok = false;
    }
    if (!ok)
      continue;
    // Separate from every other access in L.
    // Every access to obj has the same type, so its type class metadata
    // describes all of them.
    MemoryLocation oloc(obj, LocationSize::precise(bytes), si->getAAMetadata());
    for (Access &a : accs) {
      if (is_contained(mine, &a))
        continue;
      if (!AA.isNoAlias(oloc, MemoryLocation::getBeforeOrAfter(a.ptr, a.inst->getAAMetadata())))
        ok = false;
    }
    for (BasicBlock *bb : L->blocks())
      for (Instruction &I : *bb)
        if (auto *cb = dyn_cast<CallBase>(&I))
          if (isModOrRefSet(AA.getModRefInfo(cb, oloc)))
            ok = false;
    if (!ok)
      continue;
    int idx = int(P.privs.size());
    P.privs.push_back({obj, bytes, ity});
    for (Access *a : mine)
      a->priv = idx;
  }
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
  privatize(L, P, accs);
  for (Access &a : accs)
    if (!a.affine && a.priv < 0)
      rowAccess(L, P, a);
  for (Access &a : accs)
    if (a.store && !a.affine && a.family < 0 && a.priv < 0) {
      softMemReject = true;
      std::string t;
      raw_string_ostream os(t);
      SE.getSCEV(a.ptr)->print(os);
      return reject("store address is not affine in the loop's induction "
                    "or its base pointer changes in the loop [" + t + "]");
    }
  for (Family &f : P.families)
    if (f.written && !f.perIter)
      return reject("every iteration writes the same rows");

  // Same base: iteration windows of width |coef| must hold every access.
  for (size_t i = 0; i < accs.size(); i++) {
    Access &x = accs[i];
    if (!x.store || !x.affine)
      continue;
    for (size_t j = 0; j < accs.size(); j++) {
      Access &y = accs[j];
      if (!y.affine || y.baseValue != x.baseValue)
        continue;
      if (y.coef != x.coef)
        return reject("accesses to one object advance at different rates");
      size_t nle = P.leChecks.size();
      if (interleaved(L, P, x, y)) {
        P.pairs.push_back({int(i), int(j), P.leChecks.size() > nle ? "runtime" : "strided"});
        continue;
      }
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
      P.pairs.push_back({int(i), int(j), P.leChecks.size() > nle ? "runtime" : "window"});
    }
  }

  // Different objects: whole-loop byte ranges must not overlap, unless alias
  // analysis (including the Go type classes) already says they differ.
  const SCEV *btc = SE.getTruncateOrZeroExtend(tripBtc, ity);
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
  // Units: an affine object (group), a row family, or one variant load.
  struct Unit {
    Value *base = nullptr; // group
    int family = -1;
    bool store = false;
    bool variant = false;
    const SCEV *s = nullptr, *e = nullptr;
    std::vector<Access *> accs;
  };
  std::vector<Unit> units;
  for (Access &a : accs) {
    if (a.priv >= 0)
      continue; // private per chunk; disjoint from everything else (checked)
    if (a.family >= 0) {
      Unit *u = nullptr;
      for (Unit &uu : units)
        if (uu.family == a.family)
          u = &uu;
      if (!u) {
        units.push_back({});
        u = &units.back();
        u->family = a.family;
      }
      u->store |= a.store;
      u->accs.push_back(&a);
      continue;
    }
    if (!a.affine) {
      units.push_back({});
      units.back().variant = true;
      units.back().accs.push_back(&a);
      continue;
    }
    Unit *g = nullptr;
    for (Unit &gg : units)
      if (gg.family < 0 && !gg.variant && gg.base == a.baseValue)
        g = &gg;
    if (!g) {
      units.push_back({});
      g = &units.back();
      g->base = a.baseValue;
    }
    if (!a.coef || (!SE.isKnownNonNegative(a.coef) && !SE.isKnownNegative(a.coef)))
      return reject("stride of unknown sign");
    auto [s, e] = range(a);
    g->s = g->s ? SE.getUMinExpr(g->s, s) : s;
    g->e = g->e ? SE.getUMaxExpr(g->e, e) : e;
    g->store |= a.store;
    g->accs.push_back(&a);
  }
  // True when alias analysis proves every store of u disjoint from every
  // access of v.
  auto noAlias = [&](Unit &u, Unit &v) {
    for (Access *x : u.accs) {
      if (!x->store)
        continue;
      for (Access *y : v.accs)
        if (!AA.isNoAlias(
                MemoryLocation::getBeforeOrAfter(x->ptr, x->inst->getAAMetadata()),
                MemoryLocation::getBeforeOrAfter(y->ptr, y->inst->getAAMetadata())))
          return false;
    }
    return true;
  };
  std::vector<bool> inSet(units.size(), false);
  for (size_t i = 0; i < units.size(); i++) {
    Unit &u = units[i];
    if (!u.store)
      continue;
    if (u.family >= 0)
      inSet[i] = true; // its rows must be pairwise disjoint
    for (size_t j = 0; j < units.size(); j++) {
      Unit &v = units[j];
      if (i == j || (v.store && j < i))
        continue;
      auto record = [&](const char *why) {
        for (Access *x : u.accs)
          for (Access *y : v.accs)
            if (x->store)
              P.pairs.push_back({int(x - accs.data()), int(y - accs.data()), why});
      };
      if (noAlias(u, v) && (!v.store || noAlias(v, u))) {
        record("noalias");
        continue;
      }
      if (v.variant) {
        softMemReject = true;
        return reject("load through a pointer computed in the loop may alias a "
                      "stored object");
      }
      if (u.family < 0 && v.family < 0) {
        P.overlaps.push_back({u.s, u.e, v.s, v.e});
        record("runtime");
        continue;
      }
      inSet[i] = inSet[j] = true;
      record("rows");
    }
  }
  for (size_t i = 0; i < units.size(); i++) {
    if (!inSet[i])
      continue;
    RangeSource r;
    r.family = units[i].family;
    r.s = units[i].s;
    r.e = units[i].e;
    P.disjoint.push_back(r);
  }
  for (Unit &u : units) {
    Plan::AccessClass c;
    c.store = u.store;
    for (Access *a : u.accs)
      c.insts.push_back(a->inst);
    P.classes.push_back(std::move(c));
  }
  for (size_t i = 0; i < accs.size(); i++)
    if (accs[i].priv >= 0 && accs[i].store)
      for (size_t j = 0; j < accs.size(); j++)
        if (j != i)
          P.pairs.push_back({int(i), int(j), "private"});
  P.accesses = accs;
  // GPU offer: only plain objects with known whole-loop ranges.
  P.gpuOK = true;
  for (Unit &u : units) {
    if (u.family >= 0 || u.variant || !u.s || !u.e) {
      P.gpuOK = false;
      break;
    }
    bool read = false;
    for (Access *a : u.accs)
      read |= !a->store;
    P.gpuRanges.push_back({u.s, u.e, (read ? 1 : 0) | (u.store ? 2 : 0)});
  }
  if (!P.gpuOK)
    P.gpuRanges.clear();
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
        if (ignoreExits && ui && ui->getParent() != latchExit)
          continue; // check exit path, gone in the fast copy
        bool ok = false;
        for (Reduction &R : P.reds)
          if (R.next == &I)
            ok = true;
        if (!ok && recomputable(L, P, &I)) {
          if (!is_contained(P.liveOutVals, &I))
            P.liveOutVals.push_back(&I);
          ok = true;
        }
        if (!ok)
          return reject("value " + I.getName() + " is used after the loop");
      }
  return true;
}

// A value of L that depends only on affine inductions (not polynomial ones)
// and invariants, through instructions without memory access or effects.
bool Parallelizer::recomputable(Loop *L, Plan &P, Instruction *I) {
  SmallVector<Instruction *, 16> work{I};
  SmallPtrSet<Instruction *, 16> seen;
  while (!work.empty()) {
    Instruction *x = work.pop_back_val();
    if (!seen.insert(x).second || !L->contains(x))
      continue;
    if (auto *p = dyn_cast<PHINode>(x)) {
      bool iv = any_of(P.ivs, [&](const IndVar &v) { return v.phi == p && !v.poly; });
      if (!iv)
        return false;
      continue;
    }
    if (x->mayReadOrWriteMemory() || x->mayHaveSideEffects() || isa<CallBase>(x))
      return false;
    for (Value *o : x->operands())
      if (auto *oi = dyn_cast<Instruction>(o))
        work.push_back(oi);
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
  // Reduction arrays: the chunk counts into a zeroed private copy.
  SmallVector<std::pair<Value *, Value *>, 2> privPairs; // shared, private
  for (Plan::Private &pv : P.privs) {
    auto it = vmap.find(pv.object);
    Value *shared = it != vmap.end() ? static_cast<Value *>(it->second) : pv.object;
    AllocaInst *priv = b.CreateAlloca(ArrayType::get(b.getInt8Ty(), pv.bytes), nullptr,
                                      pv.object->getName() + ".priv");
    priv->setAlignment(Align(16));
    b.CreateMemSet(priv, b.getInt8(0), pv.bytes, Align(16));
    vmap[pv.object] = priv;
    privPairs.push_back({shared, priv});
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
  for (Reduction &R : P.reds) {
    auto *nphi = cast<PHINode>(vmap[R.phi]);
    nphi->setIncomingValueForBlock(entry,
                                   identity(R.kind, cast<IntegerType>(R.phi->getType())));
  }
  // Alias scopes: one per access class, in a domain of this chunk function.
  if (P.classes.size() > 1) {
    MDBuilder mdb(C);
    MDNode *domain = mdb.createAnonymousAliasScopeDomain(body->getName());
    std::vector<MDNode *> scopes;
    for (size_t i = 0; i < P.classes.size(); i++)
      scopes.push_back(mdb.createAnonymousAliasScope(domain));
    for (size_t i = 0; i < P.classes.size(); i++) {
      SmallVector<Metadata *, 8> others;
      for (size_t j = 0; j < P.classes.size(); j++)
        if (j != i && (P.classes[i].store || P.classes[j].store))
          others.push_back(scopes[j]);
      MDNode *scope = MDNode::get(C, {scopes[i]});
      MDNode *noalias = MDNode::get(C, others);
      for (Instruction *I : P.classes[i].insts) {
        auto *ni = cast<Instruction>(vmap[I]);
        ni->setMetadata(LLVMContext::MD_alias_scope,
                        MDNode::concatenate(ni->getMetadata(LLVMContext::MD_alias_scope), scope));
        if (!others.empty())
          ni->setMetadata(LLVMContext::MD_noalias,
                          MDNode::concatenate(ni->getMetadata(LLVMContext::MD_noalias), noalias));
      }
    }
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
  // Inductions become start + step*k: one counter, and the value for any
  // iteration k is exact (wrapping arithmetic, like the serial loop's).
  for (IndVar &iv : P.ivs) {
    auto *nphi = cast<PHINode>(vmap[iv.phi]);
    Value *v;
    if (iv.poly) {
      // c0 + c1*k + c2*binomial(k, 2), modulo 2^w. binomial(k, 2) =
      // k(k-1)/2 is computed from the 64-bit counter k (0 <= k < 2^63) by
      // halving the even one of k and k-1, then multiplying: exact modulo
      // 2^64, so its truncation is exact modulo 2^w. Truncating k first is
      // wrong for w < 64: k(k-1)/2 mod 2^w depends on k mod 2^(w+1) (an int16
      // recurrence over 2^20 elements shows it).
      Type *ty = iv.phi->getType();
      Value *kk = hb.CreateSExtOrTrunc(k, ty);
      v = mapped(iv.coef[0]);
      if (iv.coef.size() > 1)
        v = hb.CreateAdd(v, hb.CreateMul(mapped(iv.coef[1]), kk));
      if (iv.coef.size() > 2) {
        Value *km1 = hb.CreateSub(k, ConstantInt::get(i64, 1));
        Value *even = hb.CreateICmpEQ(hb.CreateAnd(k, ConstantInt::get(i64, 1)),
                                      ConstantInt::get(i64, 0));
        Value *a = hb.CreateSelect(even, hb.CreateLShr(k, 1), k);
        Value *b2 = hb.CreateSelect(even, km1, hb.CreateLShr(km1, 1));
        // (Go integers are at most 64 bits, so this is a truncation.)
        Value *binom = hb.CreateZExtOrTrunc(hb.CreateMul(a, b2), ty);
        v = hb.CreateAdd(v, hb.CreateMul(mapped(iv.coef[2]), binom));
      }
      v->setName(iv.phi->getName() + ".k");
      nphi->replaceAllUsesWith(v);
      nphi->eraseFromParent();
      continue;
    }
    Value *start = mapped(iv.start);
    Value *step = mapped(iv.step);
    if (iv.gepElemTy) {
      Value *n = hb.CreateMul(k, hb.CreateSExtOrTrunc(step, i64));
      v = hb.CreateGEP(iv.gepElemTy, start, n, iv.phi->getName() + ".k");
    } else {
      Value *kk = hb.CreateSExtOrTrunc(k, step->getType());
      v = hb.CreateAdd(start, hb.CreateMul(step, kk), iv.phi->getName() + ".k");
    }
    nphi->replaceAllUsesWith(v);
    nphi->eraseFromParent();
  }
  Instruction *term = latch->getTerminator();
  IRBuilder<> lb(term);
  Value *k1 = lb.CreateAdd(k, ConstantInt::get(i64, 1), "k.next");
  // rt_parallel_for only calls with lo < hi, so k.next != hi is exact and
  // gives ScalarEvolution the cheap trip count hi - lo.
  Value *more = lb.CreateICmpNE(k1, hi, "k.more");
  BranchInst *nbr = lb.CreateCondBr(more, header, exit);
  term->eraseFromParent();
  k->addIncoming(k1, latch);
  // The chunk loop's iterations are independent (Rule P). With an inner
  // loop, mark it for loop-jam (Jam.cpp).
  if (!L->getSubLoops().empty())
    nbr->setMetadata("marmot.jam", MDNode::get(C, {}));

  // Add the private reduction arrays into the shared objects.
  BasicBlock *tail = exit;
  for (size_t pi = 0; pi < privPairs.size(); pi++) {
    Plan::Private &pv = P.privs[pi];
    Value *shared = privPairs[pi].first, *priv = privPairs[pi].second;
    uint64_t n = pv.bytes / (pv.elem->getBitWidth() / 8);
    BasicBlock *loop = BasicBlock::Create(C, "priv.add", body);
    BasicBlock *put = BasicBlock::Create(C, "priv.put", body);
    BasicBlock *next = BasicBlock::Create(C, "priv.next", body);
    BasicBlock *done = BasicBlock::Create(C, "priv.done", body);
    IRBuilder<> tb(tail);
    tb.CreateBr(loop);
    IRBuilder<> lb2(loop);
    PHINode *e = lb2.CreatePHI(i64, 2, "e");
    e->addIncoming(ConstantInt::get(i64, 0), tail);
    Value *v = lb2.CreateLoad(pv.elem, lb2.CreateGEP(pv.elem, priv, e));
    lb2.CreateCondBr(lb2.CreateICmpNE(v, ConstantInt::get(pv.elem, 0)), put, next);
    IRBuilder<> pb2(put);
    pb2.CreateAtomicRMW(AtomicRMWInst::Add, pb2.CreateGEP(pv.elem, shared, e), v, MaybeAlign(),
                        AtomicOrdering::Monotonic);
    pb2.CreateBr(next);
    IRBuilder<> nb(next);
    Value *e2 = nb.CreateAdd(e, ConstantInt::get(i64, 1));
    e->addIncoming(e2, next);
    nb.CreateCondBr(nb.CreateICmpNE(e2, ConstantInt::get(i64, n)), loop, done);
    tail = done;
  }
  // Combine partial reductions.
  IRBuilder<> eb(tail);
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

// Fills two scratch arrays with the byte ranges of every family's rows and
// every listed object, and asks rt_ranges_disjoint whether they overlap.
// More than MaxRows ranges: no parallel run. Emitted before `at`, which it
// splits (the row loops need their own blocks).
constexpr uint64_t MaxRows = 4096;

Value *Parallelizer::emitRowCheck(Plan &P, SCEVExpander &exp, Instruction *at) {
  LLVMContext &C = F.getContext();
  Type *i64 = Type::getInt64Ty(C);
  Type *i8 = Type::getInt8Ty(C);
  Type *ptr = PointerType::getUnqual(C);
  Module &M = *F.getParent();
  // Expand everything first, while the dominator tree is exact (see
  // Versioning.cpp: the expander may rewrite uses for LCSSA).
  struct Ex {
    Value *s = nullptr, *e = nullptr;                                     // object
    Value *start = nullptr, *step = nullptr, *count = nullptr, *elo = nullptr, *ehi = nullptr;
  };
  std::vector<Ex> ex(P.disjoint.size());
  for (size_t k = 0; k < P.disjoint.size(); k++) {
    RangeSource &r = P.disjoint[k];
    if (r.family < 0) {
      ex[k].s = exp.expandCodeFor(r.s, i64, at);
      ex[k].e = exp.expandCodeFor(r.e, i64, at);
      continue;
    }
    Family &f = P.families[r.family];
    ex[k].start = exp.expandCodeFor(f.start, i64, at);
    ex[k].step = exp.expandCodeFor(f.step, i64, at);
    ex[k].count = exp.expandCodeFor(f.count, i64, at);
    ex[k].elo = exp.expandCodeFor(f.extLo, i64, at);
    ex[k].ehi = exp.expandCodeFor(f.extHi, i64, at);
  }
  // Count the ranges.
  IRBuilder<> b(at);
  Value *n = ConstantInt::get(i64, 0);
  std::vector<Value *> counts;
  for (size_t k = 0; k < P.disjoint.size(); k++) {
    Value *c = P.disjoint[k].family >= 0 ? b.CreateAdd(ex[k].count, ConstantInt::get(i64, 1))
                                         : ConstantInt::get(i64, 1);
    counts.push_back(c);
    n = b.CreateAdd(n, c);
  }
  Value *small = b.CreateICmpULE(n, ConstantInt::get(i64, MaxRows), "rows.small");
  // Split: at's block ends with "if small goto fill else goto join".
  BasicBlock *head = at->getParent();
  BasicBlock *join = head->splitBasicBlock(at->getIterator(), "rows.join");
  BasicBlock *fill = BasicBlock::Create(C, "rows.fill", &F, join);
  head->getTerminator()->eraseFromParent();
  IRBuilder<> hb(head);
  hb.CreateCondBr(small, fill, join);
  IRBuilder<> fb(fill);
  Value *saved = fb.CreateStackSave("rows.sp");
  Value *los = fb.CreateAlloca(i64, n, "rows.lo");
  Value *his = fb.CreateAlloca(i64, n, "rows.hi");
  Value *idx = ConstantInt::get(i64, 0);
  BasicBlock *cur = fill;
  for (size_t k = 0; k < P.disjoint.size(); k++) {
    RangeSource &r = P.disjoint[k];
    IRBuilder<> cb(cur);
    if (r.family < 0) {
      cb.CreateStore(ex[k].s, cb.CreateGEP(i64, los, idx));
      cb.CreateStore(ex[k].e, cb.CreateGEP(i64, his, idx));
      idx = cb.CreateAdd(idx, ConstantInt::get(i64, 1));
      continue;
    }
    Family &f = P.families[r.family];
    Value *table = f.table;
    Value *start = ex[k].start, *step = ex[k].step, *count = ex[k].count;
    Value *elo = ex[k].elo, *ehi = ex[k].ehi;
    BasicBlock *body = BasicBlock::Create(C, "rows.loop", &F, join);
    BasicBlock *after = BasicBlock::Create(C, "rows.next", &F, join);
    cb.CreateBr(body);
    IRBuilder<> lb(body);
    PHINode *j = lb.CreatePHI(i64, 2, "j");
    j->addIncoming(ConstantInt::get(i64, 0), cur);
    Value *slot = lb.CreateGEP(i8, table, lb.CreateAdd(start, lb.CreateMul(step, j)));
    LoadInst *row = lb.CreateLoad(ptr, slot, "row");
    row->setAAMetadata(f.rowLoad->getAAMetadata());
    Value *ri = lb.CreatePtrToInt(row, i64);
    Value *at2 = lb.CreateAdd(idx, j);
    lb.CreateStore(lb.CreateAdd(ri, elo), lb.CreateGEP(i64, los, at2));
    lb.CreateStore(lb.CreateAdd(ri, ehi), lb.CreateGEP(i64, his, at2));
    Value *j2 = lb.CreateAdd(j, ConstantInt::get(i64, 1));
    j->addIncoming(j2, body);
    BranchInst *back = lb.CreateCondBr(lb.CreateICmpULE(j2, count), body, after);
    back->setMetadata("marmot.par", MDNode::get(C, {}));
    IRBuilder<> ab(after);
    idx = ab.CreateAdd(idx, counts[k]);
    cur = after;
  }
  IRBuilder<> eb(cur);
  FunctionCallee rd = M.getOrInsertFunction(
      "rt_ranges_disjoint", FunctionType::get(Type::getInt32Ty(C), {i64, ptr, ptr}, false));
  Value *res = eb.CreateCall(rd, {n, los, his});
  eb.CreateStackRestore(saved);
  Value *okFill = eb.CreateICmpNE(res, ConstantInt::get(Type::getInt32Ty(C), 0));
  eb.CreateBr(join);
  IRBuilder<> jb(join, join->begin());
  PHINode *ok = jb.CreatePHI(Type::getInt1Ty(C), 2, "rows.ok");
  ok->addIncoming(ConstantInt::getFalse(C), head);
  ok->addIncoming(okFill, cur);
  return ok;
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

  {
    SCEVExpander exp(SE, "par");
    Instruction *at = pre->getTerminator();
    for (IndVar &iv : P.ivs)
      if (iv.poly)
        for (const SCEV *op : iv.poly->operands()) {
          Value *v = exp.expandCodeFor(op, iv.phi->getType(), at);
          iv.coef.push_back(v);
          if (!is_contained(P.liveIns, v) && !isa<Constant>(v))
            P.liveIns.push_back(v);
        }
  }
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
  // Innermost loops with reduction arrays scatter; they do not vectorize.
  bool innermost = L->getSubLoops().empty() && P.privs.empty();
  uint64_t minWork = innermost ? MinParallelWorkInner : MinParallelWork;
  uint64_t minIters = std::max<uint64_t>(2, (minWork + P.costPerIter - 1) / P.costPerIter);
  if (ForcePar)
    minIters = 1;
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
  // GPU ranges, expanded with the other conditions (before any block split).
  bool gpu = opts.gpu && P.gpuOK;
  SmallVector<std::pair<Value *, Value *>, 8> gpuVals;
  if (gpu)
    for (auto &g : P.gpuRanges) {
      Value *gs = exp.expandCodeFor(g.s, i64, at);
      Value *ge = exp.expandCodeFor(g.e, i64, at);
      gpuVals.push_back({gs, ge});
    }
  if (!P.disjoint.empty()) {
    Value *rows = emitRowCheck(P, exp, at);
    // The row check split the preheader: `at` now ends the join block.
    pre = at->getParent();
    b.SetInsertPoint(at);
    cond = b.CreateAnd(cond, rows, "par.rows");
  }
  b.SetInsertPoint(at);
  uint64_t grainWork = innermost ? GrainWorkInner : GrainWork;
  uint64_t grainIters = std::max<uint64_t>(1, (grainWork + P.costPerIter - 1) / P.costPerIter);
  if (ForcePar)
    grainIters = 1;
  // Chunks with private reduction arrays pay a clear and a merge each: about
  // one chunk per core (tc/12), not 64.
  Value *share = P.privs.empty() ? b.CreateLShr(tc, 6) : b.CreateUDiv(tc, ConstantInt::get(i64, 12));
  Value *grain = ForcePar ? static_cast<Value *>(ConstantInt::get(i64, 1))
                          : b.CreateBinaryIntrinsic(Intrinsic::umax,
                                                    ConstantInt::get(i64, grainIters), share,
                                                    nullptr, "par.grain");

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
  BasicBlock *last = par;
  if (gpu) {
    // Offer the loop to the GPU; the backend turns the call
    // into rt_gpu_for, or into false without a GPU target.
    body->addFnAttr("marmot-gpu-body");
    const DataLayout &DL = F.getDataLayout();
    StructType *rty = StructType::get(C, {ptr, i64, i64});
    ArrayType *aty = ArrayType::get(rty, gpuVals.size());
    // No memory accesses (mandelbrot's rows): no ranges, a null array.
    Value *ranges = gpuVals.empty() ? static_cast<Value *>(ConstantPointerNull::get(
                                          PointerType::getUnqual(C)))
                                    : eb.CreateAlloca(aty, nullptr, "par.ranges");
    for (size_t i = 0; i < gpuVals.size(); i++) {
      Value *slot = pb.CreateConstInBoundsGEP2_32(aty, ranges, 0, i);
      pb.CreateStore(pb.CreateIntToPtr(gpuVals[i].first, ptr), pb.CreateStructGEP(rty, slot, 0));
      pb.CreateStore(pb.CreateSub(gpuVals[i].second, gpuVals[i].first),
                     pb.CreateStructGEP(rty, slot, 1));
      pb.CreateStore(ConstantInt::get(i64, P.gpuRanges[i].flags), pb.CreateStructGEP(rty, slot, 2));
    }
    FunctionCallee gf = M.getOrInsertFunction(
        "marmot_gpu_for",
        FunctionType::get(Type::getInt1Ty(C), {ptr, i64, ptr, i64, ptr, i64, i64}, false));
    Value *done = pb.CreateCall(
        gf, {body, tc, ctx, ConstantInt::get(i64, DL.getTypeAllocSize(ctxTy)), ranges,
             ConstantInt::get(i64, gpuVals.size()), ConstantInt::get(i64, P.costPerIter)});
    BasicBlock *cpu = BasicBlock::Create(C, "par.cpu", &F, header);
    BasicBlock *join = BasicBlock::Create(C, "par.done", &F, header);
    pb.CreateCondBr(done, join, cpu);
    IRBuilder<> cb(cpu);
    cb.CreateCall(rpf, {tc, grain, body, ctx});
    cb.CreateBr(join);
    pb.SetInsertPoint(join);
    last = join;
  } else {
    pb.CreateCall(rpf, {tc, grain, body, ctx});
  }
  SmallVector<Value *, 4> results;
  for (unsigned i = 0; i < P.reds.size(); i++)
    results.push_back(pb.CreateLoad(P.reds[i].phi->getType(),
                                    pb.CreateStructGEP(ctxTy, ctx, P.liveIns.size() + i),
                                    P.reds[i].phi->getName() + ".par"));
  // Live-out values: recompute them for the last iteration k = tc - 1.
  ValueToValueMapTy lastIt;
  if (!P.liveOutVals.empty()) {
    Value *k = pb.CreateSub(tc, ConstantInt::get(i64, 1), "par.last");
    for (IndVar &iv : P.ivs) {
      if (iv.poly)
        continue;
      Value *v;
      if (iv.gepElemTy)
        v = pb.CreateGEP(iv.gepElemTy, iv.start, pb.CreateMul(k, pb.CreateSExtOrTrunc(iv.step, i64)));
      else
        v = pb.CreateAdd(iv.start,
                         pb.CreateMul(iv.step, pb.CreateSExtOrTrunc(k, iv.step->getType())));
      lastIt[iv.phi] = v;
    }
    // Clone each value's slice in dominance order.
    SmallVector<Instruction *, 16> order;
    SmallPtrSet<Instruction *, 16> seen;
    std::function<void(Instruction *)> visit = [&](Instruction *x) {
      if (!L->contains(x) || isa<PHINode>(x) || !seen.insert(x).second)
        return;
      for (Value *o : x->operands())
        if (auto *oi = dyn_cast<Instruction>(o))
          visit(oi);
      order.push_back(x);
    };
    for (Instruction *I : P.liveOutVals)
      visit(I);
    for (Instruction *x : order) {
      Instruction *c = x->clone();
      pb.Insert(c);
      RemapInstruction(c, lastIt, RF_IgnoreMissingLocals | RF_NoModuleLevelChanges);
      lastIt[x] = c;
    }
  }
  pb.CreateBr(exitBB);
  for (PHINode &phi : exitBB->phis()) {
    Value *v = phi.getIncomingValueForBlock(latch);
    Value *r = nullptr;
    for (unsigned i = 0; i < P.reds.size(); i++)
      if (P.reds[i].next == v)
        r = results[i];
    if (!r) {
      auto it = lastIt.find(v);
      if (it != lastIt.end())
        r = it->second;
      else if (!L->contains(dyn_cast<Instruction>(v)))
        r = v; // invariant
    }
    phi.addIncoming(r ? r : PoisonValue::get(phi.getType()), last);
  }
}

// Splits loop header phis of small struct type (a slice header passed in a
// parameter: phi {ptr, i64, i64}) into one phi per field and redirects
// `extractvalue phi, i` to the field phi, so ScalarEvolution sees the length
// as an induction. The aggregate phi stays for its other users.
static bool splitAggregatePhis(Function &F, LoopInfo &LI) {
  bool changed = false;
  for (Loop *L : LI.getLoopsInPreorder()) {
    BasicBlock *h = L->getHeader();
    SmallVector<PHINode *, 4> aggs;
    for (PHINode &p : h->phis())
      if (auto *st = dyn_cast<StructType>(p.getType()))
        if (st->getNumElements() <= 4 &&
            all_of(st->elements(), [](Type *t) { return t->isIntegerTy() || t->isPointerTy(); }))
          aggs.push_back(&p);
    for (PHINode *p : aggs) {
      auto *st = cast<StructType>(p->getType());
      SmallVector<ExtractValueInst *, 8> uses;
      for (User *u : p->users())
        if (auto *ev = dyn_cast<ExtractValueInst>(u); ev && ev->getNumIndices() == 1)
          uses.push_back(ev);
      if (uses.empty())
        continue;
      for (unsigned f = 0; f < st->getNumElements(); f++) {
        PHINode *fp = PHINode::Create(st->getElementType(f), p->getNumIncomingValues(),
                                      p->getName() + ".f" + Twine(f));
        fp->insertBefore(p->getIterator());
        for (unsigned i = 0; i < p->getNumIncomingValues(); i++) {
          Value *in = p->getIncomingValue(i);
          BasicBlock *bb = p->getIncomingBlock(i);
          // Look through an insertvalue chain for field f.
          Value *v = nullptr;
          for (Value *cur = in; auto *iv = dyn_cast<InsertValueInst>(cur);
               cur = iv->getAggregateOperand())
            if (iv->getNumIndices() == 1 && iv->getIndices()[0] == f) {
              v = iv->getInsertedValueOperand();
              break;
            }
          if (!v) {
            IRBuilder<> b(bb->getTerminator());
            v = b.CreateExtractValue(in, f, p->getName() + ".in" + Twine(f));
          }
          fp->addIncoming(v, bb);
        }
        for (ExtractValueInst *ev : uses)
          if (ev->getIndices()[0] == f)
            ev->replaceAllUsesWith(fp);
      }
      for (ExtractValueInst *ev : uses)
        if (ev->use_empty())
          ev->eraseFromParent();
      changed = true;
    }
  }
  return changed;
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
      if (splitAggregatePhis(*F, FAM.getResult<LoopAnalysis>(*F)))
        FAM.invalidate(*F, PreservedAnalyses::none());
      for (int round = 0; round < 64; round++) {
        auto &LI = FAM.getResult<LoopAnalysis>(*F);
        auto &SE = FAM.getResult<ScalarEvolutionAnalysis>(*F);
        auto &DT = FAM.getResult<DominatorTreeAnalysis>(*F);
        auto &AA = FAM.getResult<AAManager>(*F);
        auto &AC = FAM.getResult<AssumptionAnalysis>(*F);
        // Loops lose their preheaders and dedicated exits in the function
        // simplification pipeline; restore them.
        for (Loop *L : LI)
          simplifyLoop(L, &DT, &LI, &SE, &AC, nullptr, /*PreserveLCSSA=*/false);
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
        if (verifyFunction(*F, &errs()))
          report_fatal_error("loops: loop-parallelize produced invalid IR in " + F->getName());
        changed = true;
        // A versioned fast copy has no checks left in front of its loads:
        // hoist the invariant ones (row headers) before looking at it.
        FunctionPassManager fpm;
        fpm.addPass(createFunctionToLoopPassAdaptor(LICMPass(LICMOptions()),
                                                    /*UseMemorySSA=*/true));
        fpm.run(*F, FAM);
        FAM.invalidate(*F, PreservedAnalyses::none());
      }
    }
    for (Function &G : M)
      if (G.hasFnAttribute("marmot-par-body") && verifyFunction(G, &errs()))
        report_fatal_error("loops: invalid chunk function " + G.getName());
    if (multiversion(M, opts))
      changed = true;
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

bool forceParOption() { return ForcePar; }

} // namespace loops
} // namespace marmot

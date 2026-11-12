// Tile.cpp: loop tiling of chunk loops (rule T2).
//
// In a chunk function (Rule P), the chunk loop over i runs [lo, hi) and holds
// one inner loop over j:
//
//     for i in [lo, hi) { Fore(i); for j in [0, NJ) { Body(i, j) } }
//
// becomes
//
//     for jj = 0; jj < NJ; jj += T {
//       for i in [lo, hi) { Fore(i); for j in [jj, min(jj+T, NJ)) { Body(i, j) } }
//     }
//
// Iteration i still runs its j iterations in increasing order; different i
// are independent (Rule P proved it), so interleaving them by blocks of j
// changes nothing observable. Fore runs once per block: it must have no
// effects. Nothing may follow the inner loop in the chunk loop's body. Pays
// off when the inner loop walks one array along j and another across j
// (transpose: a[i*n+j] read, b[j*n+i] written): a block of T columns of b
// stays in cache while the rows of the chunk fill it.

#include "Internal.h"

#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/PassManager.h"
#include "llvm/IR/Verifier.h"
#include "llvm/Support/CommandLine.h"
#include "llvm/Support/ErrorHandling.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Transforms/Utils/ScalarEvolutionExpander.h"

using namespace llvm;

namespace marmot {
namespace loops {
namespace {

constexpr uint64_t TileJ = 32;

class TilePass : public PassInfoMixin<TilePass> {
public:
  explicit TilePass(Options o) : opts(o) {}
  PreservedAnalyses run(Function &F, FunctionAnalysisManager &FAM);

private:
  Options opts;
  std::string why;
  bool reject(const Twine &m) {
    why = m.str();
    return false;
  }
  bool tile(Function &F, Loop *L, ScalarEvolution &SE, DominatorTree &DT);
};

// Accesses of J: one with a stride of at least a cache line per j, one
// contiguous in j.
static bool profitable(Loop *J, ScalarEvolution &SE, const DataLayout &DL) {
  bool wide = false, unit = false;
  for (BasicBlock *bb : J->blocks())
    for (Instruction &I : *bb) {
      Value *p = nullptr;
      Type *t = nullptr;
      if (auto *ld = dyn_cast<LoadInst>(&I)) {
        p = ld->getPointerOperand();
        t = ld->getType();
      } else if (auto *st = dyn_cast<StoreInst>(&I)) {
        p = st->getPointerOperand();
        t = st->getValueOperand()->getType();
      } else {
        continue;
      }
      auto *ar = dyn_cast<SCEVAddRecExpr>(SE.getSCEV(p));
      if (!ar || ar->getLoop() != J || !ar->isAffine())
        continue;
      auto *c = dyn_cast<SCEVConstant>(ar->getStepRecurrence(SE));
      if (!c)
        continue;
      uint64_t step = c->getAPInt().abs().getZExtValue();
      if (step == DL.getTypeStoreSize(t))
        unit = true;
      else if (step >= 64)
        wide = true;
    }
  return wide && unit;
}

bool TilePass::tile(Function &F, Loop *L, ScalarEvolution &SE, DominatorTree &DT) {
  why.clear();
  if (L->getSubLoops().size() != 1)
    return reject("not one inner loop");
  Loop *J = L->getSubLoops()[0];
  if (!J->getSubLoops().empty() || !L->isLoopSimplifyForm() || !J->isLoopSimplifyForm())
    return reject("shape");
  BasicBlock *lh = L->getHeader(), *ll = L->getLoopLatch(), *lpre = L->getLoopPreheader();
  BasicBlock *jh = J->getHeader(), *jl = J->getLoopLatch(), *jpre = J->getLoopPreheader();
  BasicBlock *jexit = J->getExitBlock();
  if (L->getExitingBlock() != ll || J->getExitingBlock() != jl || !jexit || !L->getExitBlock())
    return reject("exits");
  if (!profitable(J, SE, F.getDataLayout()))
    return reject("no strided and contiguous pair of accesses");
  // J: a counted loop, trip count invariant in L, only induction phis.
  const SCEV *jbtc = SE.getBackedgeTakenCount(J);
  if (isa<SCEVCouldNotCompute>(jbtc) || !SE.isLoopInvariant(jbtc, L))
    return reject("inner trip count");
  struct IV {
    PHINode *phi;
    Value *start, *step;
  };
  SmallVector<IV, 4> ivs;
  for (PHINode &p : jh->phis()) {
    auto *nx = dyn_cast<BinaryOperator>(p.getIncomingValueForBlock(jl));
    Value *st = nullptr;
    if (p.getType()->isIntegerTy() && nx && nx->getOpcode() == Instruction::Add)
      st = nx->getOperand(0) == &p ? nx->getOperand(1) : nx->getOperand(1) == &p ? nx->getOperand(0)
                                                                                  : nullptr;
    if (!st || !L->isLoopInvariant(st))
      return reject("inner loop carries a value");
    ivs.push_back({&p, p.getIncomingValueForBlock(jpre), st});
  }
  // J's induction starts must not change with i (they are recomputed per block).
  for (IV &iv : ivs)
    if (auto *si = dyn_cast<Instruction>(iv.start); si && L->contains(si))
      if (!SE.isLoopInvariant(SE.getSCEV(si), L))
        return reject("inner induction start depends on the outer loop");
  // Fore (L blocks before J) has no effects; after J only the latch, which
  // holds nothing but L's own bookkeeping.
  for (BasicBlock *bb : L->blocks()) {
    if (J->contains(bb))
      continue;
    for (Instruction &I : *bb)
      if (I.mayHaveSideEffects() || I.mayReadOrWriteMemory())
        return reject("effects outside the inner loop");
  }
  // Blocks between J's exit and L's latch have no effects (checked above);
  // they run once per block of j instead of once per i.
  if (!jexit->phis().empty())
    return reject("values leave the inner loop");
  for (BasicBlock *bb : J->blocks())
    for (Instruction &I : *bb)
      for (User *u : I.users())
        if (!J->contains(cast<Instruction>(u)))
          return reject("values leave the inner loop");
  SCEVExpander exp(SE, "tile");
  Instruction *at = lpre->getTerminator();
  if (!exp.isSafeToExpandAt(jbtc, at))
    return reject("trip count not expandable");

  // ---- transform ----
  LLVMContext &C = F.getContext();
  Type *i64 = Type::getInt64Ty(C);
  Value *nj = exp.expandCodeFor(jbtc, i64, at);
  IRBuilder<> b(at);
  nj = b.CreateAdd(nj, ConstantInt::get(i64, 1), "tile.nj");
  // Block loop around L.
  BasicBlock *bh = BasicBlock::Create(C, "tile.jj", &F, lh);
  BasicBlock *bl = BasicBlock::Create(C, "tile.jj.next", &F, lh);
  at->setSuccessor(0, bh);
  IRBuilder<> hb(bh);
  PHINode *jj = hb.CreatePHI(i64, 2, "jj");
  jj->addIncoming(ConstantInt::get(i64, 0), lpre);
  Value *rest = hb.CreateSub(nj, jj);
  Value *bsize = hb.CreateBinaryIntrinsic(Intrinsic::umin, rest, ConstantInt::get(i64, TileJ),
                                          nullptr, "tile.bs");
  hb.CreateBr(lh);
  // L's header phis now enter from the block loop with their start values.
  for (PHINode &p : lh->phis())
    p.setIncomingBlock(p.getBasicBlockIndex(lpre), bh);
  // L's exit goes to the next block instead.
  BasicBlock *lexit = L->getExitBlock();
  auto *lbr = cast<BranchInst>(ll->getTerminator());
  for (unsigned s = 0; s < lbr->getNumSuccessors(); s++)
    if (lbr->getSuccessor(s) == lexit)
      lbr->setSuccessor(s, bl);
  IRBuilder<> nb(bl);
  Value *jj2 = nb.CreateAdd(jj, ConstantInt::get(i64, TileJ));
  jj->addIncoming(jj2, bl);
  nb.CreateCondBr(nb.CreateICmpULT(jj2, nj), bh, lexit);
  for (PHINode &p : lexit->phis())
    p.setIncomingBlock(p.getBasicBlockIndex(ll), bl);
  // J: a counter c in [0, bsize), inductions at index jj + c.
  IRBuilder<> jb(jh, jh->getFirstNonPHIIt());
  PHINode *c = PHINode::Create(i64, 2, "tile.c");
  c->insertBefore(jh->begin());
  c->addIncoming(ConstantInt::get(i64, 0), jpre);
  Value *idx = jb.CreateAdd(jj, c, "tile.j");
  for (IV &iv : ivs) {
    Value *k = jb.CreateSExtOrTrunc(idx, iv.step->getType());
    Value *v = jb.CreateAdd(iv.start, jb.CreateMul(iv.step, k), iv.phi->getName() + ".t");
    iv.phi->replaceAllUsesWith(v);
    iv.phi->eraseFromParent();
  }
  auto *jbr = cast<BranchInst>(jl->getTerminator());
  IRBuilder<> lb(jbr);
  Value *c2 = lb.CreateAdd(c, ConstantInt::get(i64, 1));
  c->addIncoming(c2, jl);
  lb.CreateCondBr(lb.CreateICmpNE(c2, bsize), jh, jexit);
  jbr->eraseFromParent();
  if (opts.remarks)
    errs() << "loops: " << F.getName() << ": tiled loop " << lh->getName() << " by " << TileJ
           << " inner iterations\n";
  return true;
}

} // namespace
// Off by default: on bench transpose tiling lost to loop-jam (it takes the
// chunk loop jam would use; 1.02 s vs 0.49 s wall).
static cl::opt<bool> TileOn("loops-tile", cl::Hidden,
                            cl::desc("Loop optimizer: tile chunk loops (experimental)"));
namespace {

PreservedAnalyses TilePass::run(Function &F, FunctionAnalysisManager &FAM) {
  if (!TileOn || !F.hasFnAttribute("marmot-par-body"))
    return PreservedAnalyses::all();
  auto &LI = FAM.getResult<LoopAnalysis>(F);
  auto &SE = FAM.getResult<ScalarEvolutionAnalysis>(F);
  auto &DT = FAM.getResult<DominatorTreeAnalysis>(F);
  // The chunk loop is the only top-level loop.
  if (LI.getTopLevelLoops().size() != 1)
    return PreservedAnalyses::all();
  Loop *L = LI.getTopLevelLoops()[0];
  if (!tile(F, L, SE, DT)) {
    if (opts.remarks && !why.empty())
      errs() << "loops: " << F.getName() << ": tile: no: " << why << "\n";
    return PreservedAnalyses::all();
  }
  if (verifyFunction(F, &errs()))
    report_fatal_error("loops: tiling produced invalid IR in " + F.getName());
  return PreservedAnalyses::none();
}

} // namespace

void addTile(FunctionPassManager &fpm, const Options &opts) { fpm.addPass(TilePass(opts)); }

bool tileOption() { return TileOn; }

} // namespace loops
} // namespace marmot

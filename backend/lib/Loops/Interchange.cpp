// Interchange.cpp: reduction interchange (Rule I in docs/loops.md).
//
// Rewrites
//
//     for j in J {                      // loop J
//       s := init
//       for k in K { s = op(s, ...) }   // loop K, one float recurrence s
//       C[j] = s
//     }
//
// into
//
//     for j in J { C[j] = init }
//     for k in K {
//       for j in J { C[j] = op(C[j], ...) }
//     }
//
// Element j sees exactly the same operations, with the same operands, in the
// same k order: C[j] holds s after each k step. So the result is bit
// identical, floating point included. The new inner loop runs over j, where
// matmul's accesses are contiguous, so LLVM vectorizes it.
//
// Legal when: K's trip count and every value K's body uses except j, k and
// s are the same for all j; nothing in J writes memory except the final
// store; and alias analysis proves the store to C disjoint from every load in
// J (in chunk functions the alias scopes of Rule P prove it).

#include "Internal.h"

#include "llvm/Analysis/AliasAnalysis.h"
#include "llvm/Analysis/LoopInfo.h"
#include "llvm/Analysis/MemoryLocation.h"
#include "llvm/Analysis/ScalarEvolution.h"
#include "llvm/Analysis/ScalarEvolutionExpressions.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/IntrinsicInst.h"
#include "llvm/IR/PassManager.h"
#include "llvm/Support/raw_ostream.h"
#include "llvm/Transforms/Utils/BasicBlockUtils.h"
#include "llvm/Transforms/Utils/Cloning.h"
#include "llvm/Transforms/Utils/LoopSimplify.h"
#include "llvm/Transforms/Utils/LoopUtils.h"
#include "llvm/Transforms/Utils/ScalarEvolutionExpander.h"

using namespace llvm;

namespace marmot {
namespace loops {
namespace {

struct IV {
  PHINode *phi;
  Value *start, *step; // phi = start + step*n in iteration n
};

class Interchanger {
public:
  Interchanger(Function &F, LoopInfo &LI, ScalarEvolution &SE, DominatorTree &DT, AAResults &AA,
               const Options &opts)
      : F(F), LI(LI), SE(SE), DT(DT), AA(AA), opts(opts) {}
  bool tryLoop(Loop *J);

private:
  Function &F;
  LoopInfo &LI;
  ScalarEvolution &SE;
  DominatorTree &DT;
  AAResults &AA;
  const Options &opts;
  std::string why;
  bool reject(const Twine &m) {
    why = m.str();
    return false;
  }
  bool inductionOf(Loop *L, PHINode *phi, IV &iv);
};

bool Interchanger::inductionOf(Loop *L, PHINode *phi, IV &iv) {
  if (!phi->getType()->isIntegerTy())
    return false;
  auto *next = dyn_cast<BinaryOperator>(phi->getIncomingValueForBlock(L->getLoopLatch()));
  if (!next || next->getOpcode() != Instruction::Add)
    return false;
  Value *step = next->getOperand(0) == phi ? next->getOperand(1)
                : next->getOperand(1) == phi ? next->getOperand(0)
                                             : nullptr;
  if (!step || !L->isLoopInvariant(step))
    return false;
  iv = {phi, phi->getIncomingValueForBlock(L->getLoopPreheader()), step};
  return true;
}

bool Interchanger::tryLoop(Loop *J) {
  auto note = [&](bool ok) {
    if (opts.remarks)
      errs() << "loops: " << F.getName() << ": interchange loop " << J->getHeader()->getName()
             << ": " << (ok ? "done" : "no: " + why) << "\n";
    return ok;
  };
  if (J->getSubLoops().size() != 1)
    return false; // not a candidate; no remark
  Loop *K = J->getSubLoops()[0];
  if (!K->getSubLoops().empty())
    return false;
  if (!J->isLoopSimplifyForm() || !K->isLoopSimplifyForm())
    return note(reject("not in simplified form"));
  if (J->getExitingBlock() != J->getLoopLatch() || K->getExitingBlock() != K->getLoopLatch() ||
      !J->getExitBlock() || !K->getExitBlock())
    return note(reject("loops with more than one exit"));
  // K's body is a straight chain of blocks from its header to its latch
  // (removed checks leave such chains behind).
  SmallVector<BasicBlock *, 8> chain;
  for (BasicBlock *bb = K->getHeader();;) {
    chain.push_back(bb);
    if (bb == K->getLoopLatch())
      break;
    bb = bb->getSingleSuccessor();
    if (!bb || !K->contains(bb) || chain.size() > K->getNumBlocks())
      return note(reject("inner loop body has branches"));
  }
  if (chain.size() != K->getNumBlocks())
    return note(reject("inner loop body has branches"));
  BasicBlock *kb = K->getHeader();
  BasicBlock *klatch = K->getLoopLatch();

  // Header phis: J has only its induction; K has its induction and one
  // floating point recurrence.
  IV jv{nullptr, nullptr, nullptr}, kv{nullptr, nullptr, nullptr};
  unsigned nphi = 0;
  for (PHINode &p : J->getHeader()->phis()) {
    nphi++;
    if (!inductionOf(J, &p, jv))
      return note(reject("outer loop carries a value other than its induction"));
  }
  if (nphi != 1)
    return note(reject("outer loop has more than one induction"));
  // J's body outside K is a straight chain: every block but the latch has
  // one successor, so each part runs once per j.
  for (BasicBlock *bb : J->blocks())
    if (!K->contains(bb) && bb != J->getLoopLatch() && !bb->getSingleSuccessor())
      return note(reject("branch in the outer loop body"));
  PHINode *s = nullptr;
  bool haveK = false;
  for (PHINode &p : kb->phis()) {
    IV t;
    if (!haveK && inductionOf(K, &p, t)) {
      kv = t;
      haveK = true;
    } else if (p.getType()->isFloatingPointTy() && !s) {
      s = &p;
    } else {
      return note(reject("inner loop carries more than one value"));
    }
  }
  if (!haveK || !jv.phi || !s)
    return note(reject("no float recurrence in the inner loop"));
  // s' = op(s, ...) with s used exactly once.
  auto *next = dyn_cast<Instruction>(s->getIncomingValueForBlock(klatch));
  if (!next || !K->contains(next) || !s->hasOneUse() || *s->user_begin() != next)
    return note(reject("recurrence shape"));
  bool fma = false;
  if (auto *ii = dyn_cast<IntrinsicInst>(next))
    fma = ii->getIntrinsicID() == Intrinsic::fma || ii->getIntrinsicID() == Intrinsic::fmuladd;
  if (!fma && !isa<BinaryOperator>(next))
    return note(reject("recurrence is not an arithmetic operation"));
  // s' feeds the next iteration and the exit value only.
  for (User *u : next->users())
    if (u != s && !(isa<PHINode>(u) && cast<Instruction>(u)->getParent() == K->getExitBlock()))
      return note(reject("recurrence value used inside the loop"));
  Value *init = s->getIncomingValueForBlock(K->getLoopPreheader());
  if (!J->isLoopInvariant(init))
    return note(reject("initial value depends on the outer induction"));

  // K's trip count is the same for every j.
  const SCEV *kbtc = SE.getBackedgeTakenCount(K);
  const SCEV *jbtc = SE.getBackedgeTakenCount(J);
  if (isa<SCEVCouldNotCompute>(kbtc) || !SE.isLoopInvariant(kbtc, J) ||
      isa<SCEVCouldNotCompute>(jbtc))
    return note(reject("trip counts"));

  // J's body outside K: no memory writes except one store of the exit value.
  StoreInst *st = nullptr;
  for (BasicBlock *bb : J->blocks()) {
    for (Instruction &I : *bb) {
      if (auto *sti = dyn_cast<StoreInst>(&I)) {
        if (st || K->contains(sti) || !sti->isSimple())
          return note(reject("more than one store"));
        st = sti;
        continue;
      }
      if (I.mayWriteToMemory() || I.mayThrow())
        return note(reject("side effects in the nest"));
      if (auto *cb = dyn_cast<CallBase>(&I))
        if (!cb->doesNotAccessMemory() && !isa<DbgInfoIntrinsic>(cb))
          return note(reject("call in the nest"));
      if (isa<AllocaInst>(I))
        return note(reject("alloca in the nest"));
    }
  }
  if (!st)
    return note(reject("no store"));
  auto *lcssa = dyn_cast<PHINode>(st->getValueOperand());
  if (!lcssa || lcssa->getParent() != K->getExitBlock() || lcssa->getNumIncomingValues() != 1 ||
      lcssa->getIncomingValue(0) != next)
    return note(reject("store of something other than the result"));
  // The store's address is affine in j with stride = element size, and the
  // same for every k (it is outside K).
  const SCEV *sa = SE.getSCEV(st->getPointerOperand());
  auto *sar = dyn_cast<SCEVAddRecExpr>(sa);
  uint64_t esz = F.getDataLayout().getTypeStoreSize(st->getValueOperand()->getType());
  if (!sar || sar->getLoop() != J || !sar->isAffine() ||
      sar->getStepRecurrence(SE) != SE.getConstant(sar->getType(), esz))
    return note(reject("store is not contiguous in the outer induction"));
  // Profitable when K reads something contiguous in j (the vector loop).
  bool contiguous = false;
  for (BasicBlock *cb : chain)
  for (Instruction &I : *cb)
    if (auto *ld = dyn_cast<LoadInst>(&I)) {
      const SCEV *la = SE.getSCEVAtScope(ld->getPointerOperand(), J);
      (void)la;
      const SCEV *l = SE.getSCEV(ld->getPointerOperand());
      // contiguous in j: the address advances by the element size per j
      // step; look for an AddRec of J anywhere in the expression.
      if (auto *ar = dyn_cast<SCEVAddRecExpr>(l))
        if (ar->getLoop() == K && SE.isLoopInvariant(ar->getStepRecurrence(SE), J)) {
          const SCEV *st0 = ar->getStart();
          if (auto *jr = dyn_cast<SCEVAddRecExpr>(st0))
            if (jr->getLoop() == J &&
                jr->getStepRecurrence(SE) ==
                    SE.getConstant(jr->getType(), F.getDataLayout().getTypeStoreSize(ld->getType())))
              contiguous = true;
        }
      // b[k][j]: the row pointer is loaded in K, the offset is 8*j.
      const SCEV *base = SE.getPointerBase(l);
      const SCEV *off = SE.getMinusSCEV(l, base);
      if (auto *jr = dyn_cast<SCEVAddRecExpr>(off))
        if (jr->getLoop() == J &&
            jr->getStepRecurrence(SE) ==
                SE.getConstant(jr->getType(), F.getDataLayout().getTypeStoreSize(ld->getType())))
          contiguous = true;
    }
  if (!contiguous)
    return note(reject("no load contiguous in the outer induction"));
  // Loads in the nest never read what the store writes.
  MemoryLocation sloc = MemoryLocation::getBeforeOrAfter(st->getPointerOperand(),
                                                         st->getAAMetadata());
  for (BasicBlock *bb : J->blocks())
    for (Instruction &I : *bb)
      if (auto *ld = dyn_cast<LoadInst>(&I))
        if (!AA.isNoAlias(sloc, MemoryLocation::getBeforeOrAfter(ld->getPointerOperand(),
                                                                 ld->getAAMetadata())))
          return note(reject("a load may read the stored array"));

  // Expandable trip counts in J's preheader.
  BasicBlock *jpre = J->getLoopPreheader();
  SCEVExpander exp(SE, "ric");
  if (!exp.isSafeToExpandAt(kbtc, jpre->getTerminator()) ||
      !exp.isSafeToExpandAt(jbtc, jpre->getTerminator()))
    return note(reject("trip counts not expandable"));

  // Values the new loops need from J's body (outside K): instructions in J
  // (not K) that K's body or the store address use. They must be free of
  // side effects; they are recomputed in the new inner loop.
  SetVector<Instruction *> need;
  SmallVector<Value *, 16> work;
  for (BasicBlock *bb : chain)
    for (Instruction &I : *bb)
      for (Value *op : I.operands())
        work.push_back(op);
  work.push_back(st->getPointerOperand());
  while (!work.empty()) {
    auto *I = dyn_cast<Instruction>(work.pop_back_val());
    if (!I || !J->contains(I) || K->contains(I) || I == jv.phi || need.count(I))
      continue;
    if (isa<PHINode>(I) || I->mayReadOrWriteMemory())
      return note(reject("outer body value the inner loop needs is not pure"));
    need.insert(I);
    for (Value *op : I->operands())
      work.push_back(op);
  }

  // ---- transform ----
  LLVMContext &C = F.getContext();
  Type *i64 = Type::getInt64Ty(C);
  BasicBlock *jexit = J->getExitBlock();
  // No value of J is used after it (the store is its only effect).
  for (BasicBlock *bb : J->blocks())
    for (Instruction &I : *bb)
      for (User *u : I.users())
        if (!J->contains(cast<Instruction>(u)))
          return note(reject("value used after the loop"));
  Instruction *at = jpre->getTerminator();
  Value *jn = exp.expandCodeFor(SE.getAddExpr(SE.getTruncateOrZeroExtend(jbtc, i64),
                                              SE.getOne(i64)),
                                i64, at);
  Value *kn = exp.expandCodeFor(SE.getAddExpr(SE.getTruncateOrZeroExtend(kbtc, i64),
                                              SE.getOne(i64)),
                                i64, at);
  Value *k0 = kv.start, *kstep = kv.step;
  if (!J->isLoopInvariant(k0) || !J->isLoopInvariant(kstep))
    return note(reject("inner induction depends on the outer one"));

  // Blocks: init loop, k loop header, j loop body, k latch, done.
  BasicBlock *initB = BasicBlock::Create(C, "ric.init", &F, jexit);
  BasicBlock *kh = BasicBlock::Create(C, "ric.k", &F, jexit);
  BasicBlock *jb = BasicBlock::Create(C, "ric.j", &F, jexit);
  BasicBlock *kl = BasicBlock::Create(C, "ric.klatch", &F, jexit);
  auto jAt = [&](IRBuilder<> &b, Value *n) {
    Value *nn = b.CreateSExtOrTrunc(n, jv.step->getType());
    return b.CreateAdd(jv.start, b.CreateMul(jv.step, nn), "j");
  };
  auto kAt = [&](IRBuilder<> &b, Value *n) {
    Value *nn = b.CreateSExtOrTrunc(n, kstep->getType());
    return b.CreateAdd(k0, b.CreateMul(kstep, nn), "k");
  };
  // Clone the needed outer-body values and the store address for a given j.
  auto cloneNeeded = [&](IRBuilder<> &b, Value *jval, ValueToValueMapTy &vm) {
    vm[jv.phi] = jval;
    for (Instruction *I : need) {
      // `need` is in reverse discovery order; clone in dominance order.
      (void)I;
    }
    SmallVector<Instruction *, 16> order(need.begin(), need.end());
    llvm::sort(order, [&](Instruction *a, Instruction *b2) {
      if (a->getParent() == b2->getParent())
        return a->comesBefore(b2);
      return DT.dominates(a->getParent(), b2->getParent());
    });
    for (Instruction *I : order) {
      Instruction *c = I->clone();
      b.Insert(c);
      RemapInstruction(c, vm, RF_IgnoreMissingLocals | RF_NoModuleLevelChanges);
      vm[I] = c;
    }
  };

  // init loop: C[j] = init for every j.
  {
    IRBuilder<> b(initB);
    PHINode *n = b.CreatePHI(i64, 2, "n");
    n->addIncoming(ConstantInt::get(i64, 0), jpre);
    ValueToValueMapTy vm;
    cloneNeeded(b, jAt(b, n), vm);
    Value *addr = st->getPointerOperand();
    auto it = vm.find(addr);
    if (it != vm.end())
      addr = it->second;
    StoreInst *s0 = b.CreateStore(init, addr);
    s0->setAAMetadata(st->getAAMetadata());
    s0->setAlignment(st->getAlign());
    Value *n2 = b.CreateAdd(n, ConstantInt::get(i64, 1));
    n->addIncoming(n2, initB);
    b.CreateCondBr(b.CreateICmpNE(n2, jn), initB, kh);
  }
  // k loop.
  PHINode *kn0;
  {
    IRBuilder<> b(kh);
    kn0 = b.CreatePHI(i64, 2, "kn");
    kn0->addIncoming(ConstantInt::get(i64, 0), initB);
    b.CreateBr(jb);
  }
  // j loop body: C[j] = op(C[j], ...) for this k.
  {
    IRBuilder<> b(jb);
    PHINode *n = b.CreatePHI(i64, 2, "n");
    n->addIncoming(ConstantInt::get(i64, 0), kh);
    ValueToValueMapTy vm;
    cloneNeeded(b, jAt(b, n), vm);
    vm[kv.phi] = kAt(b, kn0);
    Value *addr = st->getPointerOperand();
    auto it = vm.find(addr);
    if (it != vm.end())
      addr = it->second;
    LoadInst *acc = b.CreateLoad(s->getType(), addr, "acc");
    acc->setAAMetadata(st->getAAMetadata());
    acc->setAlignment(st->getAlign());
    vm[s] = acc;
    Instruction *lastNext = nullptr;
    for (BasicBlock *cb : chain)
    for (Instruction &I : *cb) {
      if (isa<PHINode>(I) || I.isTerminator())
        continue;
      // The inner induction's own increment and exit test are not needed.
      Instruction *c = I.clone();
      b.Insert(c);
      RemapInstruction(c, vm, RF_IgnoreMissingLocals | RF_NoModuleLevelChanges);
      vm[&I] = c;
      if (&I == next)
        lastNext = c;
    }
    StoreInst *s1 = b.CreateStore(lastNext, addr);
    s1->setAAMetadata(st->getAAMetadata());
    s1->setAlignment(st->getAlign());
    Value *n2 = b.CreateAdd(n, ConstantInt::get(i64, 1));
    n->addIncoming(n2, jb);
    b.CreateCondBr(b.CreateICmpNE(n2, jn), jb, kl);
  }
  {
    IRBuilder<> b(kl);
    Value *k2 = b.CreateAdd(kn0, ConstantInt::get(i64, 1));
    kn0->addIncoming(k2, kl);
    b.CreateCondBr(b.CreateICmpNE(k2, kn), kh, jexit);
  }
  // Enter the new nest instead of J. J's blocks become unreachable.
  BasicBlock *oldHeader = J->getHeader();
  BasicBlock *oldLatch = J->getLoopLatch();
  at->replaceUsesOfWith(oldHeader, initB);
  for (PHINode &p : jexit->phis()) {
    // J had no live-outs; exit phis can only be LCSSA phis without users.
    int idx = p.getBasicBlockIndex(oldLatch);
    if (idx >= 0)
      p.setIncomingBlock(idx, kl);
  }
  SmallVector<BasicBlock *, 16> dead(J->blocks().begin(), J->blocks().end());
  DeleteDeadBlocks(dead);
  return note(true);
}

class InterchangePass : public PassInfoMixin<InterchangePass> {
public:
  explicit InterchangePass(Options o) : opts(o) {}
  PreservedAnalyses run(Function &F, FunctionAnalysisManager &FAM) {
    bool changed = false;
    for (int round = 0; round < 16; round++) {
      auto &LI = FAM.getResult<LoopAnalysis>(F);
      auto &SE = FAM.getResult<ScalarEvolutionAnalysis>(F);
      auto &DT = FAM.getResult<DominatorTreeAnalysis>(F);
      auto &AA = FAM.getResult<AAManager>(F);
      Interchanger ic(F, LI, SE, DT, AA, opts);
      bool did = false;
      for (Loop *L : LI.getLoopsInPreorder())
        if (ic.tryLoop(L)) {
          did = true;
          break;
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

void addInterchange(FunctionPassManager &fpm, const Options &opts) {
  fpm.addPass(InterchangePass(opts));
}

} // namespace loops
} // namespace marmot

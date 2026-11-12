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

#include "llvm/Analysis/AliasAnalysis.h"
#include "llvm/Analysis/AssumptionCache.h"
#include "llvm/Analysis/MemoryLocation.h"
#include "llvm/Analysis/ValueTracking.h"
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
#include "llvm/Transforms/Scalar/LoopLoadElimination.h"
#include "llvm/Transforms/Utils/Cloning.h"
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
  bool scan(Function &F, Loop *L, ScalarEvolution &SE, LoopInfo &LI, DominatorTree &DT,
            AAResults &AA);
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


// Scan: an innermost counted loop with one integer accumulator r and one
// store, r = r op x(i); q(i) = r, op in add, xor, or, and (associative),
// becomes
//
//     size = n / 64; if size < MinScanBlock: the original loop
//     pass 1: sums[b] = op of x(i) over block b          (b = 0..63)
//     serial: sums[b] = r0 op sums[0..b-1] (exclusive)
//     pass 2: r = sums[b]; for i in block b: r = r op x(i); q(i) = r
//     tail:   the iterations past 64*size, serially
//
// Rule P then runs pass 1 and pass 2 in parallel. Every q(i) gets r0 op x(0)
// op ... op x(i), the serial loop's value, because op is associative. x(i)
// is computed twice: it must be free of effects, and its loads must read
// either q(i) itself (before it is written, as in the original iteration) or
// memory the store cannot touch.
constexpr uint64_t MinScanBlock = 8192; // 64K element scans gained nothing (bench prefixsum)
constexpr unsigned ScanBlocks = 64;

bool IdiomPass::scan(Function &F, Loop *L, ScalarEvolution &SE, LoopInfo &LI, DominatorTree &DT,
                     AAResults &AA) {
  if (!L->getSubLoops().empty() || !L->isLoopSimplifyForm())
    return false;
  BasicBlock *latch = L->getLoopLatch(), *pre = L->getLoopPreheader();
  BasicBlock *exit = L->getExitBlock();
  if (L->getExitingBlock() != latch || !exit)
    return false;
  // The original loop stays as the fallback for small n: do not split it again.
  if (latch->getTerminator()->getMetadata("marmot.scan"))
    return false;
  const SCEV *btc = SE.getBackedgeTakenCount(L);
  if (isa<SCEVCouldNotCompute>(btc) || btc->getType()->getIntegerBitWidth() > 64)
    return false;
  formLCSSA(*L, DT, &LI, &SE);
  // Header phis: integer inductions (next = phi + step) and one accumulator.
  struct IV {
    PHINode *phi;
    Value *start, *step;
  };
  SmallVector<IV, 4> ivs;
  PHINode *r = nullptr;
  for (PHINode &p : L->getHeader()->phis()) {
    auto *nx = dyn_cast<BinaryOperator>(p.getIncomingValueForBlock(latch));
    if (p.getType()->isIntegerTy() && nx && nx->getOpcode() == Instruction::Add &&
        (nx->getOperand(0) == &p || nx->getOperand(1) == &p)) {
      Value *st = nx->getOperand(0) == &p ? nx->getOperand(1) : nx->getOperand(0);
      if (L->isLoopInvariant(st)) {
        ivs.push_back({&p, p.getIncomingValueForBlock(pre), st});
        continue;
      }
    }
    if (r || !p.getType()->isIntegerTy())
      return false;
    r = &p;
  }
  if (!r)
    return false;
  auto *rn = dyn_cast<BinaryOperator>(r->getIncomingValueForBlock(latch));
  if (!rn || !L->contains(rn))
    return false;
  Instruction::BinaryOps op = rn->getOpcode();
  if (op != Instruction::Add && op != Instruction::Xor && op != Instruction::Or &&
      op != Instruction::And)
    return false;
  Value *x = rn->getOperand(0) == r ? rn->getOperand(1) : rn->getOperand(1) == r ? rn->getOperand(0)
                                                                                  : nullptr;
  if (!x || x == r || !r->hasOneUse())
    return false;
  // One store of rn, contiguous in the induction.
  StoreInst *st = nullptr;
  for (BasicBlock *bb : L->blocks())
    for (Instruction &I : *bb) {
      if (auto *s2 = dyn_cast<StoreInst>(&I)) {
        if (st || !s2->isSimple() || s2->getValueOperand() != rn)
          return false;
        st = s2;
      } else if (auto *cb = dyn_cast<CallBase>(&I)) {
        if (!cb->doesNotAccessMemory() || !cb->willReturn())
          return false;
      } else if (I.mayWriteToMemory() || isa<AllocaInst>(I) || I.mayThrow()) {
        return false;
      }
    }
  if (!st)
    return false;
  auto *qa = dyn_cast<SCEVAddRecExpr>(SE.getSCEV(st->getPointerOperand()));
  uint64_t esz = F.getDataLayout().getTypeStoreSize(rn->getType());
  if (!qa || qa->getLoop() != L || !qa->isAffine() ||
      qa->getStepRecurrence(SE) != SE.getConstant(qa->getType(), esz))
    return false;
  // rn: used by r, the store and exit phis only.
  for (User *u : rn->users()) {
    auto *ui = cast<Instruction>(u);
    if (ui != r && ui != st && !(isa<PHINode>(ui) && ui->getParent() == exit))
      return false;
  }
  for (BasicBlock *bb : L->blocks())
    for (Instruction &I : *bb)
      if (&I != rn)
        for (User *u : I.users())
          if (!L->contains(cast<Instruction>(u)))
            return false;
  // Slices of x and of the store address: instructions of L (not phis
  // other than inductions), all executed every iteration.
  SmallVector<Instruction *, 16> slice;
  SmallPtrSet<Instruction *, 16> seen;
  std::function<bool(Value *)> collect = [&](Value *v) -> bool {
    auto *I = dyn_cast<Instruction>(v);
    if (!I || !L->contains(I) || seen.count(I))
      return true;
    if (auto *p = dyn_cast<PHINode>(I))
      return any_of(ivs, [&](const IV &iv) { return iv.phi == p; });
    if (I == rn || I == st || !DT.dominates(I->getParent(), latch))
      return false;
    if (auto *ld = dyn_cast<LoadInst>(I)) {
      if (!ld->isSimple())
        return false;
      // Reads q(i) itself, or memory the store cannot write.
      bool same = SE.getSCEV(ld->getPointerOperand()) == qa;
      bool apart = AA.isNoAlias(MemoryLocation::get(ld), MemoryLocation::getBeforeOrAfter(
                                                             st->getPointerOperand(),
                                                             st->getAAMetadata()));
      if (!same && !apart)
        return false;
    } else if (I->mayReadOrWriteMemory() || I->mayThrow()) {
      return false;
    }
    for (Value *o : I->operands())
      if (!collect(o))
        return false;
    seen.insert(I);
    slice.push_back(I);
    return true;
  };
  if (!collect(x) || !collect(st->getPointerOperand()))
    return false;
  // A recurrence over the inductions alone (no loads) is a polynomial
  // induction; Rule P runs it in one pass.
  if (none_of(slice, [](Instruction *I) { return isa<LoadInst>(I); }))
    return false;
  SCEVExpander exp(SE, "scan");
  Instruction *at = pre->getTerminator();
  if (!exp.isSafeToExpandAt(btc, at))
    return false;

  // ---- transform ----
  LLVMContext &C = F.getContext();
  Type *i64 = Type::getInt64Ty(C);
  Type *ty = r->getType();
  Constant *ident = op == Instruction::And ? ConstantInt::getAllOnesValue(ty)
                                           : ConstantInt::get(ty, 0);
  Value *n = exp.expandCodeFor(btc, i64, at);
  IRBuilder<> b(at);
  n = b.CreateAdd(n, ConstantInt::get(i64, 1), "scan.n");
  // max(n/64, 1): known positive, so Rule P can prove the block windows of
  // pass 2 disjoint (n/64 may be 0 as far as ScalarEvolution knows; then the
  // original loop runs anyway).
  Value *size = b.CreateBinaryIntrinsic(Intrinsic::umax,
                                        b.CreateUDiv(n, ConstantInt::get(i64, ScanBlocks)),
                                        ConstantInt::get(i64, 1), nullptr, "scan.size");
  Value *big = b.CreateICmpUGE(size, ConstantInt::get(i64, MinScanBlock), "scan.big");
  Value *r0 = r->getIncomingValueForBlock(pre);
  IRBuilder<> eb(&*F.getEntryBlock().getFirstInsertionPt());
  AllocaInst *sums = eb.CreateAlloca(ArrayType::get(ty, ScanBlocks), nullptr, "scan.sums");
  BasicBlock *header = L->getHeader();
  auto block = [&](const char *name) { return BasicBlock::Create(C, name, &F, header); };
  BasicBlock *p1h = block("scan.p1"), *p1i = block("scan.p1.in"), *p1l = block("scan.p1.next");
  BasicBlock *sh = block("scan.offsets"), *p2h = block("scan.p2"), *p2i = block("scan.p2.in");
  BasicBlock *p2l = block("scan.p2.next"), *th = block("scan.tail.check"), *tl = block("scan.tail");
  BasicBlock *done = block("scan.done");
  b.CreateCondBr(big, p1h, header);
  at->eraseFromParent();
  latch->getTerminator()->setMetadata("marmot.scan", MDNode::get(C, {}));
  // One iteration's x (and address) for index idx.
  auto iteration = [&](IRBuilder<> &ib, Value *idx, Value *&xv, Value *&addr) {
    ValueToValueMapTy vm;
    for (IV &iv : ivs) {
      Value *k = ib.CreateSExtOrTrunc(idx, iv.step->getType());
      vm[iv.phi] = ib.CreateAdd(iv.start, ib.CreateMul(iv.step, k));
    }
    for (Instruction *I : slice) {
      Instruction *c = I->clone();
      ib.Insert(c);
      RemapInstruction(c, vm, RF_IgnoreMissingLocals | RF_NoModuleLevelChanges);
      vm[I] = c;
    }
    auto get = [&](Value *v) -> Value * {
      auto it = vm.find(v);
      return it != vm.end() ? static_cast<Value *>(it->second) : v;
    };
    xv = get(x);
    addr = get(st->getPointerOperand());
  };
  auto slot = [&](IRBuilder<> &ib, Value *bi) {
    return ib.CreateInBoundsGEP(ArrayType::get(ty, ScanBlocks), sums,
                                {ConstantInt::get(i64, 0), bi});
  };
  // Pass 1: block sums.
  {
    IRBuilder<> hb(p1h);
    PHINode *bi = hb.CreatePHI(i64, 2, "b");
    bi->addIncoming(ConstantInt::get(i64, 0), pre);
    hb.CreateBr(p1i);
    IRBuilder<> ib(p1i);
    PHINode *j = ib.CreatePHI(i64, 2, "j");
    PHINode *sv = ib.CreatePHI(ty, 2, "s");
    j->addIncoming(ConstantInt::get(i64, 0), p1h);
    sv->addIncoming(ident, p1h);
    Value *idx = ib.CreateAdd(ib.CreateMul(bi, size), j);
    Value *xv, *addr;
    iteration(ib, idx, xv, addr);
    Value *s2 = ib.CreateBinOp(op, sv, xv);
    Value *j2 = ib.CreateAdd(j, ConstantInt::get(i64, 1));
    j->addIncoming(j2, p1i);
    sv->addIncoming(s2, p1i);
    ib.CreateCondBr(ib.CreateICmpNE(j2, size), p1i, p1l);
    IRBuilder<> lb(p1l);
    lb.CreateStore(s2, slot(lb, bi));
    Value *b2 = lb.CreateAdd(bi, ConstantInt::get(i64, 1));
    bi->addIncoming(b2, p1l);
    lb.CreateCondBr(lb.CreateICmpNE(b2, ConstantInt::get(i64, ScanBlocks)), p1h, sh);
  }
  // Exclusive offsets, serially.
  Value *accEnd;
  {
    IRBuilder<> hb(sh);
    PHINode *bi = hb.CreatePHI(i64, 2, "b");
    PHINode *acc = hb.CreatePHI(ty, 2, "acc");
    bi->addIncoming(ConstantInt::get(i64, 0), p1l);
    acc->addIncoming(r0, p1l);
    Value *sl = slot(hb, bi);
    Value *t = hb.CreateLoad(ty, sl);
    hb.CreateStore(acc, sl);
    accEnd = hb.CreateBinOp(op, acc, t, "acc.next");
    Value *b2 = hb.CreateAdd(bi, ConstantInt::get(i64, 1));
    bi->addIncoming(b2, sh);
    acc->addIncoming(accEnd, sh);
    hb.CreateCondBr(hb.CreateICmpNE(b2, ConstantInt::get(i64, ScanBlocks)), sh, p2h);
  }
  // Pass 2: block scans from their offsets.
  {
    IRBuilder<> hb(p2h);
    PHINode *bi = hb.CreatePHI(i64, 2, "b");
    bi->addIncoming(ConstantInt::get(i64, 0), sh);
    Value *off = hb.CreateLoad(ty, slot(hb, bi), "off");
    hb.CreateBr(p2i);
    IRBuilder<> ib(p2i);
    PHINode *j = ib.CreatePHI(i64, 2, "j");
    PHINode *rv = ib.CreatePHI(ty, 2, "r");
    j->addIncoming(ConstantInt::get(i64, 0), p2h);
    rv->addIncoming(off, p2h);
    Value *idx = ib.CreateAdd(ib.CreateMul(bi, size), j);
    Value *xv, *addr;
    iteration(ib, idx, xv, addr);
    Value *r2 = ib.CreateBinOp(op, rv, xv);
    StoreInst *s3 = ib.CreateStore(r2, addr);
    s3->setAAMetadata(st->getAAMetadata());
    s3->setAlignment(st->getAlign());
    Value *j2 = ib.CreateAdd(j, ConstantInt::get(i64, 1));
    j->addIncoming(j2, p2i);
    rv->addIncoming(r2, p2i);
    ib.CreateCondBr(ib.CreateICmpNE(j2, size), p2i, p2l);
    IRBuilder<> lb(p2l);
    Value *b2 = lb.CreateAdd(bi, ConstantInt::get(i64, 1));
    bi->addIncoming(b2, p2l);
    lb.CreateCondBr(lb.CreateICmpNE(b2, ConstantInt::get(i64, ScanBlocks)), p2h, th);
  }
  // Tail: iterations 64*size .. n-1, continuing from the last block's end.
  Value *covered;
  {
    IRBuilder<> hb(th);
    covered = hb.CreateMul(size, ConstantInt::get(i64, ScanBlocks));
    hb.CreateCondBr(hb.CreateICmpULT(covered, n), tl, done);
    IRBuilder<> ib(tl);
    PHINode *idx = ib.CreatePHI(i64, 2, "i");
    PHINode *rv = ib.CreatePHI(ty, 2, "r");
    idx->addIncoming(covered, th);
    rv->addIncoming(accEnd, th);
    Value *xv, *addr;
    iteration(ib, idx, xv, addr);
    Value *r2 = ib.CreateBinOp(op, rv, xv);
    StoreInst *s3 = ib.CreateStore(r2, addr);
    s3->setAAMetadata(st->getAAMetadata());
    s3->setAlignment(st->getAlign());
    Value *i2 = ib.CreateAdd(idx, ConstantInt::get(i64, 1));
    idx->addIncoming(i2, tl);
    rv->addIncoming(r2, tl);
    ib.CreateCondBr(ib.CreateICmpNE(i2, n), tl, done);
    IRBuilder<> db(done);
    PHINode *fin = db.CreatePHI(ty, 2, "scan.r");
    fin->addIncoming(accEnd, th);
    fin->addIncoming(r2, tl);
    db.CreateBr(exit);
    for (PHINode &p : exit->phis())
      p.addIncoming(fin, done); // exit phis carry rn, the final value
  }
  // The generated block and tail loops are scan shaped too: never split them.
  MDNode *mark = MDNode::get(C, {});
  for (BasicBlock *bb : {p1i, p1l, sh, p2i, p2l, tl})
    bb->getTerminator()->setMetadata("marmot.scan", mark);
  if (opts.remarks)
    errs() << "loops: " << F.getName() << ": scan loop split into parallel blocks\n";
  return true;
}

PreservedAnalyses IdiomPass::run(Function &F, FunctionAnalysisManager &FAM) {
  if (F.isDeclaration() || F.hasOptNone())
    return PreservedAnalyses::all();
  bool changed = false;
  // Forward stores to loads of the next iteration (a[i] += a[i-1] becomes a
  // register recurrence) so scans show their accumulator.
  {
    FunctionPassManager fpm;
    fpm.addPass(LoopLoadEliminationPass());
    fpm.run(F, FAM);
  }
  for (int round = 0; round < 16; round++) {
    auto &LI = FAM.getResult<LoopAnalysis>(F);
    auto &SE = FAM.getResult<ScalarEvolutionAnalysis>(F);
    auto &DT = FAM.getResult<DominatorTreeAnalysis>(F);
    auto &AC = FAM.getResult<AssumptionAnalysis>(F);
    for (Loop *L : LI)
      simplifyLoop(L, &DT, &LI, &SE, &AC, nullptr, /*PreserveLCSSA=*/false);
    auto &AA = FAM.getResult<AAManager>(F);
    bool did = false;
    for (Loop *L : LI.getLoopsInPreorder())
      if ((ruleOn(Rule::ByteCount) && byteCount(F, L, SE, LI, DT)) ||
          (ruleOn(Rule::Scan) && scan(F, L, SE, LI, DT, AA))) {
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

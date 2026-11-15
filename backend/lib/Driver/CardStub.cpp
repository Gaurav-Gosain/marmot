// Card barrier stubs: each call of marmot.card(ptr slot, i1 on), which the
// front end emits per card site with MARMOT_WB=cardstub, becomes inline asm
// that tests the flag and calls the runtime stub for the register holding
// the slot (rt_card_x5, rt_card_rax). The asm names that register through an
// operand modifier, so the register allocator picks it freely. The stubs
// keep every register except those the asm declares clobbered: arm64 x16,
// x17 (linker veneers may use them before the stub runs), x30 and the flags;
// x86-64 the flags. On x86-64 the call pushes, so the function gets no red
// zone. The call is not a safepoint and calls nothing.

#include "llvm/ADT/PostOrderIterator.h"
#include "llvm/ADT/SmallPtrSet.h"
#include "llvm/IR/CFG.h"
#include "llvm/IR/Dominators.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/IntrinsicInst.h"
#include "llvm/IR/InlineAsm.h"
#include "llvm/IR/Instructions.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/PatternMatch.h"
#include "llvm/TargetParser/Triple.h"

#include <cstdlib>


// Card sites between two calls share one load of the marking flag. The front
// end loads rt_gc_marking (volatile) at every site; the flag only changes
// while the goroutine is stopped at a call, so a load that dominates a later
// one with no call on any path between them gives the same value. Only calls
// count: marmot.card itself, inline asm and intrinsics never stop the
// goroutine.
static bool stopsHere(const llvm::Instruction &i, const llvm::Function *card) {
  auto *cb = llvm::dyn_cast<llvm::CallBase>(&i);
  if (!cb || llvm::isa<llvm::IntrinsicInst>(cb) || cb->isInlineAsm())
    return false;
  return cb->getCalledFunction() != card;
}

static bool cleanRange(llvm::BasicBlock::const_iterator from, llvm::BasicBlock::const_iterator to,
                       const llvm::Function *card) {
  for (auto it = from; it != to; ++it)
    if (stopsHere(*it, card))
      return false;
  return true;
}

// No call on any path from a to b (a dominates b).
static bool noCallBetween(const llvm::Instruction *a, const llvm::Instruction *b, const llvm::Function *card) {
  const llvm::BasicBlock *ab = a->getParent(), *bb = b->getParent();
  if (ab == bb && a->comesBefore(b))
    return cleanRange(std::next(a->getIterator()), b->getIterator(), card);
  if (!cleanRange(std::next(a->getIterator()), ab->end(), card) ||
      !cleanRange(bb->begin(), b->getIterator(), card))
    return false;
  llvm::SmallPtrSet<const llvm::BasicBlock *, 32> fwd, back;
  llvm::SmallVector<const llvm::BasicBlock *, 32> work(llvm::succ_begin(ab), llvm::succ_end(ab));
  while (!work.empty()) {
    const llvm::BasicBlock *x = work.pop_back_val();
    if (fwd.insert(x).second)
      work.append(llvm::succ_begin(x), llvm::succ_end(x));
  }
  work.assign(llvm::pred_begin(bb), llvm::pred_end(bb));
  while (!work.empty()) {
    const llvm::BasicBlock *x = work.pop_back_val();
    if (back.insert(x).second)
      work.append(llvm::pred_begin(x), llvm::pred_end(x));
  }
  for (const llvm::BasicBlock *x : fwd)
    if (back.count(x) && !cleanRange(x->begin(), x->end(), card))
      return false;
  return true;
}

static void shareFlagLoads(llvm::Function &f, const llvm::GlobalVariable *flagVar, const llvm::Function *card) {
  std::vector<llvm::LoadInst *> loads;
  llvm::ReversePostOrderTraversal<llvm::Function *> rpo(&f);
  for (llvm::BasicBlock *bb : rpo)
    for (llvm::Instruction &i : *bb)
      if (auto *ld = llvm::dyn_cast<llvm::LoadInst>(&i);
          ld && ld->getPointerOperand()->stripPointerCasts() == flagVar && ld->getType()->isIntegerTy(8))
        loads.push_back(ld);
  if (loads.size() < 2)
    return;
  llvm::DominatorTree dt(f);
  std::vector<llvm::LoadInst *> kept;
  for (llvm::LoadInst *ld : loads) {
    llvm::LoadInst *same = nullptr;
    // The most recent few kept loads: enough for straight-line stretches.
    for (size_t k = kept.size(), n = 0; k > 0 && n < 8 && !same; --k, ++n)
      if (dt.dominates(kept[k - 1], ld) && noCallBetween(kept[k - 1], ld, card))
        same = kept[k - 1];
    if (!same) {
      kept.push_back(ld);
      continue;
    }
    ld->replaceAllUsesWith(same);
    ld->eraseFromParent();
  }
}

bool marmotLowerCardStubs(llvm::Module &m) {
  llvm::Function *card = m.getFunction("marmot.card");
  if (!card)
    return true;
  llvm::Triple triple(m.getTargetTriple());
  bool arm64 = triple.getArch() == llvm::Triple::aarch64;
  bool x86 = triple.getArch() == llvm::Triple::x86_64;
  if (!arm64 && !x86) {
    m.getContext().emitError("marmot.card: card stubs exist only for arm64 and x86-64");
    return false;
  }
  // MARMOT_CARDSTUB_CALL=1: every site calls rt_cardt_<reg>, which tests the
  // flag itself (one call instruction per site instead of a test and a call).
  static const bool callAlways = [] {
    const char *e = getenv("MARMOT_CARDSTUB_CALL");
    return e && *e == '1';
  }();
  std::string prefix = std::string(triple.isOSBinFormatMachO() ? "_" : "") + (callAlways ? "rt_cardt_" : "rt_card_");
  llvm::LLVMContext &ctx = m.getContext();
  llvm::Type *ptr = llvm::PointerType::get(ctx, 0);
  llvm::Type *flag = arm64 ? llvm::Type::getInt32Ty(ctx) : llvm::Type::getInt8Ty(ctx);
  auto *fty = callAlways ? llvm::FunctionType::get(llvm::Type::getVoidTy(ctx), {ptr}, false)
                         : llvm::FunctionType::get(llvm::Type::getVoidTy(ctx), {ptr, flag}, false);
  std::string text, constraints;
  if (arm64) {
    text = callAlways ? "bl " + prefix + "${0:x}" : "cbz ${1:w}, 1f\n\tbl " + prefix + "${0:x}\n1:";
    constraints = callAlways ? "r,~{x16},~{x17},~{lr},~{cc}" : "r,r,~{x16},~{x17},~{lr},~{cc}";
    // x18 is the platform register on Darwin and never allocated there;
    // elsewhere it is a temporary with no stub.
    if (!triple.isOSDarwin())
      constraints += ",~{x18}";
  } else {
    text = callAlways ? "call " + prefix + "${0:V}" : "testb $1, $1\n\tjz 1f\n\tcall " + prefix + "${0:V}\n1:";
    constraints = callAlways ? "r,~{cc},~{dirflag},~{fpsr},~{flags}" : "r,r,~{cc},~{dirflag},~{fpsr},~{flags}";
  }
  llvm::InlineAsm *asmv = llvm::InlineAsm::get(fty, text, constraints, /*hasSideEffects=*/true);
  const llvm::GlobalVariable *flagVar = m.getNamedGlobal("rt_gc_marking");
  if (flagVar && !callAlways) {
    llvm::SmallPtrSet<llvm::Function *, 32> fns;
    for (llvm::User *u : card->users())
      if (auto *c = llvm::dyn_cast<llvm::CallInst>(u))
        fns.insert(c->getFunction());
    for (llvm::Function *f : fns)
      shareFlagLoads(*f, flagVar, card);
  }
  std::vector<llvm::CallInst *> calls;
  for (llvm::User *u : card->users())
    if (auto *c = llvm::dyn_cast<llvm::CallInst>(u); c && c->getCalledOperand() == card)
      calls.push_back(c);
  for (llvm::CallInst *c : calls) {
    llvm::IRBuilder<> b(c);
    llvm::Value *slot = c->getArgOperand(0);
    if (slot->getType() != ptr)
      slot = b.CreateAddrSpaceCast(slot, ptr);
    // The flag usually arrives as icmp ne (load), 0: test the loaded value
    // itself, which the stub's test does anyway.
    llvm::Value *on = c->getArgOperand(1);
    if (auto *cmp = llvm::dyn_cast<llvm::ICmpInst>(on);
        cmp && cmp->getPredicate() == llvm::ICmpInst::ICMP_NE && llvm::PatternMatch::match(cmp->getOperand(1), llvm::PatternMatch::m_Zero()) &&
        cmp->getOperand(0)->getType()->isIntegerTy() && cmp->getOperand(0)->getType()->getIntegerBitWidth() <= flag->getIntegerBitWidth())
      on = cmp->getOperand(0);
    llvm::Value *flagArg = c->getArgOperand(1);
    llvm::CallInst *a;
    if (callAlways) {
      a = b.CreateCall(fty, asmv, {slot});
    } else {
      on = b.CreateZExt(on, flag);
      a = b.CreateCall(fty, asmv, {slot, on});
    }
    a->addFnAttr(llvm::Attribute::NoUnwind);
    if (x86)
      c->getFunction()->addFnAttr(llvm::Attribute::NoRedZone);
    c->eraseFromParent();
    // A flag test nothing uses any more, and its load (volatile, but only
    // the card sites read the flag this way), go too.
    if (auto *cmp = llvm::dyn_cast<llvm::ICmpInst>(flagArg); cmp && cmp->use_empty()) {
      llvm::Value *v = cmp->getOperand(0);
      cmp->eraseFromParent();
      flagArg = v;
    }
    if (auto *ld = llvm::dyn_cast<llvm::LoadInst>(flagArg);
        ld && ld->use_empty() && flagVar && ld->getPointerOperand()->stripPointerCasts() == flagVar)
      ld->eraseFromParent();
  }
  if (card->use_empty())
    card->eraseFromParent();
  return true;
}

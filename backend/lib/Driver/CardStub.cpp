// Card barrier stubs: each call of marmot.card(ptr slot, i1 on), which the
// front end emits per card site with MARMOT_WB=cardstub, becomes inline asm
// that tests the flag and calls the runtime stub for the register holding
// the slot (rt_card_x5, rt_card_rax). The asm names that register through an
// operand modifier, so the register allocator picks it freely. The stubs
// keep every register except those the asm declares clobbered: arm64 x16,
// x17 (linker veneers may use them before the stub runs), x30 and the flags;
// x86-64 the flags. On x86-64 the call pushes, so the function gets no red
// zone. The call is not a safepoint and calls nothing.

#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/InlineAsm.h"
#include "llvm/IR/Instructions.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/PatternMatch.h"
#include "llvm/TargetParser/Triple.h"

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
  std::string prefix = triple.isOSBinFormatMachO() ? "_rt_card_" : "rt_card_";
  llvm::LLVMContext &ctx = m.getContext();
  llvm::Type *ptr = llvm::PointerType::get(ctx, 0);
  llvm::Type *flag = arm64 ? llvm::Type::getInt32Ty(ctx) : llvm::Type::getInt8Ty(ctx);
  auto *fty = llvm::FunctionType::get(llvm::Type::getVoidTy(ctx), {ptr, flag}, false);
  std::string text, constraints;
  if (arm64) {
    text = "cbz ${1:w}, 1f\n\tbl " + prefix + "${0:x}\n1:";
    constraints = "r,r,~{x16},~{x17},~{lr},~{cc}";
    // x18 is the platform register on Darwin and never allocated there;
    // elsewhere it is a temporary with no stub.
    if (!triple.isOSDarwin())
      constraints += ",~{x18}";
  } else {
    text = "testb $1, $1\n\tjz 1f\n\tcall " + prefix + "${0:V}\n1:";
    constraints = "r,r,~{cc},~{dirflag},~{fpsr},~{flags}";
  }
  llvm::InlineAsm *asmv = llvm::InlineAsm::get(fty, text, constraints, /*hasSideEffects=*/true);
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
    on = b.CreateZExt(on, flag);
    llvm::CallInst *a = b.CreateCall(fty, asmv, {slot, on});
    a->addFnAttr(llvm::Attribute::NoUnwind);
    if (x86)
      c->getFunction()->addFnAttr(llvm::Attribute::NoRedZone);
    c->eraseFromParent();
  }
  if (card->use_empty())
    card->eraseFromParent();
  return true;
}

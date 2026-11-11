// RuntimeBitcode.cpp: see RuntimeBitcode.h.
#include "marmot/Driver/RuntimeBitcode.h"

#include "llvm/ADT/DenseMap.h"
#include "llvm/ADT/SmallPtrSet.h"
#include "llvm/Bitcode/BitcodeReader.h"
#include "llvm/Bitcode/BitcodeWriter.h"
#include "llvm/IR/Constants.h"
#include "llvm/IR/LLVMContext.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/Operator.h"
#include "llvm/Linker/Linker.h"
#include "llvm/Support/MemoryBuffer.h"
#include "llvm/Support/raw_ostream.h"

using namespace llvm;

namespace marmot {

namespace {

// Collects the global values a function body references, looking through
// constant expressions.
void collectRefs(const Function &f, SmallPtrSetImpl<const GlobalValue *> &out) {
  SmallVector<const Constant *, 16> work;
  SmallPtrSet<const Constant *, 16> seen;
  for (const BasicBlock &bb : f)
    for (const Instruction &inst : bb)
      for (const Use &op : inst.operands())
        if (auto *c = dyn_cast<Constant>(op.get()))
          if (seen.insert(c).second)
            work.push_back(c);
  while (!work.empty()) {
    const Constant *c = work.pop_back_val();
    if (auto *gv = dyn_cast<GlobalValue>(c)) {
      out.insert(gv);
      continue;
    }
    for (const Use &op : c->operands())
      if (auto *cc = dyn_cast<Constant>(op.get()))
        if (seen.insert(cc).second)
          work.push_back(cc);
  }
}

class Classifier {
public:
  // True if f, or an internal function reachable from f, touches an internal
  // global variable or alias.
  bool touchesLocalState(const Function *f) {
    auto it = memo.find(f);
    if (it != memo.end())
      return it->second;
    memo[f] = false; // cycles: assume clean, the other path decides
    SmallPtrSet<const GlobalValue *, 16> refs;
    collectRefs(*f, refs);
    bool bad = false;
    for (const GlobalValue *gv : refs) {
      if (isa<GlobalVariable>(gv) || isa<GlobalAlias>(gv) || isa<GlobalIFunc>(gv)) {
        if (gv->hasLocalLinkage())
          bad = true;
      } else if (auto *callee = dyn_cast<Function>(gv)) {
        if (callee->hasLocalLinkage() && !callee->isDeclaration() &&
            touchesLocalState(callee))
          bad = true;
      }
      if (bad)
        break;
    }
    memo[f] = bad;
    return bad;
  }

private:
  DenseMap<const Function *, bool> memo;
};

} // namespace

bool prepareRuntimeBitcode(const std::string &path, RuntimeLib &lib,
                           std::string &err) {
  auto buf = MemoryBuffer::getFile(path);
  if (!buf) {
    err = path + ": " + buf.getError().message();
    return false;
  }
  LLVMContext ctx;
  auto modOr = parseBitcodeFile((*buf)->getMemBufferRef(), ctx);
  if (!modOr) {
    err = path + ": " + toString(modOr.takeError());
    return false;
  }
  Module &m = **modOr;

  Classifier cls;
  SmallVector<Function *, 64> toDeclare;
  for (Function &f : m) {
    if (f.isDeclaration() || f.hasLocalLinkage())
      continue;
    bool keep = !f.hasFnAttribute(Attribute::NoInline) &&
                !f.hasFnAttribute(Attribute::Naked) && !f.isInterposable() &&
                !cls.touchesLocalState(&f);
    if (keep)
      lib.importable.insert(f.getName());
    else
      toDeclare.push_back(&f);
  }
  // Imported bodies must not drag stack protectors into Go functions.
  for (Function &f : m) {
    f.removeFnAttr(Attribute::StackProtect);
    f.removeFnAttr(Attribute::StackProtectStrong);
    f.removeFnAttr(Attribute::StackProtectReq);
  }
  for (Function *f : toDeclare) {
    f->deleteBody();
    f->setComdat(nullptr);
  }
  for (GlobalVariable &gv : m.globals()) {
    if (gv.isDeclaration() || gv.hasLocalLinkage())
      continue;
    gv.setInitializer(nullptr);
    gv.setLinkage(GlobalValue::ExternalLinkage);
    gv.setComdat(nullptr);
  }
  // Internal functions and globals that are now unreferenced are dropped by
  // the linker (LinkOnlyNeeded only pulls what the destination needs).

  SmallString<0> out;
  raw_svector_ostream os(out);
  WriteBitcodeToFile(m, os);
  lib.bitcode = MemoryBuffer::getMemBufferCopy(StringRef(out.data(), out.size()),
                                               "rtcore.prepared.bc");
  return true;
}

bool linkRuntimeBitcode(Module &m, const RuntimeLib &rt, std::string &err) {
  auto rtModOr = getLazyBitcodeModule(rt.bitcode->getMemBufferRef(), m.getContext());
  if (!rtModOr) {
    err = "runtime bitcode: " + toString(rtModOr.takeError());
    return false;
  }
  std::unique_ptr<Module> rtMod = std::move(*rtModOr);
  rtMod->setTargetTriple(m.getTargetTriple());
  rtMod->setDataLayout(m.getDataLayout());
  if (Linker::linkModules(m, std::move(rtMod), Linker::Flags::LinkOnlyNeeded)) {
    err = "linking runtime bitcode failed";
    return false;
  }
  for (Function &f : m)
    if (!f.isDeclaration() && rt.importable.contains(f.getName()))
      f.setLinkage(GlobalValue::AvailableExternallyLinkage);
  return true;
}

} // namespace marmot

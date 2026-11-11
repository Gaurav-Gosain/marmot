// BCE.cpp: bounds check and nil check elimination. The rules are written
// down in docs/passes.md (section go-bce) and this file implements exactly
// those rules; keep them in sync.
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/Dominance.h"
#include "mlir/IR/Matchers.h"

namespace marmot::go {
#define GEN_PASS_DEF_GOBCE
#include "marmot/Dialect/Go/Passes.h.inc"
} // namespace marmot::go

using namespace mlir;
using namespace marmot::go;

namespace {

std::optional<APInt> constInt(Value v) {
  APInt c;
  if (matchPattern(v, m_ConstantInt(&c)))
    return c;
  return std::nullopt;
}

// E1: two values are the same Go value when they are the same SSA value,
// equal integer constants of the same type, or llvm.extractvalue at the same
// position of the same SSA aggregate.
bool sameValue(Value a, Value b) {
  if (a == b)
    return true;
  if (a.getType() != b.getType())
    return false;
  auto ca = constInt(a), cb = constInt(b);
  if (ca && cb)
    return *ca == *cb;
  auto ea = a.getDefiningOp<LLVM::ExtractValueOp>();
  auto eb = b.getDefiningOp<LLVM::ExtractValueOp>();
  return ea && eb && ea.getContainer() == eb.getContainer() &&
         ea.getPosition() == eb.getPosition();
}

// B1: constants with 0 <= i < n (as signed values, so n is a valid length).
bool constantInRange(Value i, Value n) {
  auto ci = constInt(i), cn = constInt(n);
  return ci && cn && !ci->isNegative() && ci->slt(*cn);
}

// B3: i is a block argument of a header block H, and
//  (a) every edge into H passes for i either an integer constant >= 0 or
//      `llvm.add %i, 1` (either operand order) whose block is dominated by
//      the guard's true successor T;
//  (b) H ends in `llvm.cond_br %c, ^T, ^F` with %c = `llvm.icmp "slt"|"ult"
//      %i, %m` (i on the left);
//  (c) T has H as its only predecessor and T dominates the check (and T != F);
//  (d) %m is the same value as the check's length n (E1).
// Then at the check 0 <= i < m = n.
bool inductionInRange(Value i, Value n, Operation *check, DominanceInfo &dom) {
  auto arg = dyn_cast<BlockArgument>(i);
  if (!arg)
    return false;
  Block *h = arg.getOwner();
  if (h->isEntryBlock())
    return false;
  auto br = dyn_cast<LLVM::CondBrOp>(h->getTerminator());
  if (!br)
    return false;
  auto cmp = br.getCondition().getDefiningOp<LLVM::ICmpOp>();
  if (!cmp || cmp.getLhs() != i ||
      (cmp.getPredicate() != LLVM::ICmpPredicate::slt &&
       cmp.getPredicate() != LLVM::ICmpPredicate::ult))
    return false;
  if (!sameValue(cmp.getRhs(), n))
    return false;
  Block *t = br.getTrueDest();
  if (t == br.getFalseDest() || t->getSinglePredecessor() != h ||
      !dom.dominates(t, check->getBlock()))
    return false;
  unsigned idx = arg.getArgNumber();
  for (auto it = h->pred_begin(); it != h->pred_end(); ++it) {
    Block *pred = *it;
    auto bri = dyn_cast<BranchOpInterface>(pred->getTerminator());
    if (!bri)
      return false;
    for (unsigned s = 0; s < pred->getTerminator()->getNumSuccessors(); ++s) {
      if (pred->getTerminator()->getSuccessor(s) != h)
        continue;
      SuccessorOperands ops = bri.getSuccessorOperands(s);
      if (idx >= ops.size() || ops.isOperandProduced(idx))
        return false;
      Value in = ops[idx];
      if (auto c = constInt(in)) {
        if (c->isNegative())
          return false;
        continue;
      }
      auto add = in.getDefiningOp<LLVM::AddOp>();
      if (!add)
        return false;
      Value other = add.getLhs() == i ? add.getRhs() : add.getRhs() == i ? add.getLhs() : Value();
      auto step = other ? constInt(other) : std::nullopt;
      if (!step || !step->isOne() || !dom.dominates(t, add->getBlock()))
        return false;
    }
  }
  return true;
}

// N1: pointers that are never nil.
bool knownNonNil(Value p) {
  Operation *def = p.getDefiningOp();
  if (!def)
    return false;
  if (isa<NewOp, BoxOp, MakeClosureOp, LLVM::AllocaOp, LLVM::AddressOfOp>(def))
    return true;
  if (auto call = dyn_cast<LLVM::CallOp>(def))
    if (auto callee = call.getCallee())
      return *callee == "runtime.newobject" || *callee == "runtime.mallocgc";
  return false;
}

struct GoBCEPass : marmot::go::impl::GoBCEBase<GoBCEPass> {
  void runOnOperation() override {
    getOperation().walk([&](LLVM::LLVMFuncOp fn) {
      if (fn.isExternal())
        return;
      DominanceInfo dom(fn);
      SmallVector<Operation *> kept;     // checks that stay, in walk order
      SmallVector<Operation *> toErase;
      fn.walk([&](Operation *op) {
        Value i, n;
        if (auto bc = dyn_cast<BoundsCheckOp>(op)) {
          i = bc.getIndex();
          n = bc.getLen();
        } else if (auto ix = dyn_cast<IndexOp>(op)) {
          if (ix.getNocheck())
            return;
          i = ix.getIndex();
          n = ix.getLen();
        } else if (auto nc = dyn_cast<NilCheckOp>(op)) {
          Value p = nc.getPtr();
          bool remove = knownNonNil(p);
          // N2: a nil check of the same pointer dominates this one.
          for (Operation *k : kept)
            if (!remove)
              if (auto knc = dyn_cast<NilCheckOp>(k))
                remove = knc.getPtr() == p && dom.properlyDominates(k, op);
          if (remove)
            toErase.push_back(op);
          else
            kept.push_back(op);
          return;
        } else {
          return;
        }
        bool remove = constantInRange(i, n) || inductionInRange(i, n, op, dom);
        // B2: a bounds check of the same index and length dominates this one.
        for (Operation *k : kept) {
          if (remove)
            break;
          Value ki, kn;
          if (auto kb = dyn_cast<BoundsCheckOp>(k)) {
            ki = kb.getIndex();
            kn = kb.getLen();
          } else if (auto kx = dyn_cast<IndexOp>(k)) {
            ki = kx.getIndex();
            kn = kx.getLen();
          } else {
            continue;
          }
          remove = sameValue(ki, i) && sameValue(kn, n) && dom.properlyDominates(k, op);
        }
        if (!remove) {
          kept.push_back(op);
          return;
        }
        if (auto ix = dyn_cast<IndexOp>(op))
          ix.setNocheck(true);
        else
          toErase.push_back(op);
      });
      for (Operation *op : toErase)
        op->erase();
    });
  }
};

} // namespace

// AggregateCopies.cpp: see the pass description in Passes.td.
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/Builders.h"
#include "mlir/Interfaces/DataLayoutInterfaces.h"
#include "mlir/Interfaces/SideEffectInterfaces.h"

namespace marmot::go {
#define GEN_PASS_DEF_AGGREGATECOPIES
#include "marmot/Dialect/Go/Passes.h.inc"
} // namespace marmot::go

using namespace mlir;

namespace {

bool isAggregate(Type t) { return isa<LLVM::LLVMStructType, LLVM::LLVMArrayType>(t); }

// Runtime allocation functions: they return fresh memory and write nothing
// that Go code can already see.
bool isAllocCall(Operation *op) {
  auto call = dyn_cast<LLVM::CallOp>(op);
  if (!call || !call.getCallee())
    return false;
  StringRef n = *call.getCallee();
  return n == "runtime.newobject" || n == "runtime.makeslice" ||
         n == "runtime.mallocgc" || n == "runtime.mallocConservative";
}

bool mayWrite(Operation *op) {
  if (isAllocCall(op))
    return false;
  if (auto mem = dyn_cast<MemoryEffectOpInterface>(op))
    return mem.hasEffect<MemoryEffects::Write>();
  return true; // unknown effects
}

// True if nothing between from (exclusive) and to (exclusive), both in the
// same block, may write memory.
bool noWritesBetween(Operation *from, Operation *to) {
  if (from->getBlock() != to->getBlock() || !from->isBeforeInBlock(to))
    return false;
  for (Operation *op = from->getNextNode(); op != to; op = op->getNextNode())
    if (mayWrite(op))
      return false;
  return true;
}

struct AggregateCopiesPass
    : marmot::go::impl::AggregateCopiesBase<AggregateCopiesPass> {
  using AggregateCopiesBase::AggregateCopiesBase;

  void runOnOperation() override {
    ModuleOp m = getOperation();
    DataLayout dl = DataLayout::closest(m);
    OpBuilder b(m.getContext());
    Type ptrTy = LLVM::LLVMPointerType::get(m.getContext());
    Type i64 = b.getI64Type();
    auto size = [&](Type t) -> uint64_t { return isAggregate(t) ? dl.getTypeSize(t) : 0; };

    SmallVector<LLVM::LoadOp> loads;
    SmallVector<LLVM::StoreOp> zeroStores;
    m.walk([&](Operation *op) {
      if (auto ld = dyn_cast<LLVM::LoadOp>(op)) {
        if (!ld.getVolatile_() && size(ld.getType()) >= minSize)
          loads.push_back(ld);
      } else if (auto st = dyn_cast<LLVM::StoreOp>(op)) {
        if (!st.getVolatile_() && size(st.getValue().getType()) >= minSize &&
            st.getValue().getDefiningOp<LLVM::ZeroOp>())
          zeroStores.push_back(st);
      }
    });

    for (LLVM::StoreOp st : zeroStores) {
      b.setInsertionPoint(st);
      Location loc = st.getLoc();
      Value zero = LLVM::ConstantOp::create(b, loc, b.getI8Type(), 0);
      Value n = LLVM::ConstantOp::create(b, loc, i64, size(st.getValue().getType()));
      LLVM::MemsetOp::create(b, loc, st.getAddr(), zero, n, /*isVolatile=*/false);
      st.erase();
    }

    for (LLVM::LoadOp ld : loads) {
      Type t = ld.getType();
      uint64_t n = size(t);
      bool ok = true;
      for (Operation *user : ld->getUsers()) {
        if (auto st = dyn_cast<LLVM::StoreOp>(user)) {
          ok = st.getValue() == ld.getResult() && !st.getVolatile_() &&
               noWritesBetween(ld, st);
        } else if (auto ev = dyn_cast<LLVM::ExtractValueOp>(user)) {
          ok = true;
        } else {
          ok = false;
        }
        if (!ok)
          break;
      }
      if (!ok)
        continue;
      for (Operation *user : llvm::make_early_inc_range(ld->getUsers())) {
        if (auto st = dyn_cast<LLVM::StoreOp>(user)) {
          b.setInsertionPoint(st);
          Value len = LLVM::ConstantOp::create(b, st.getLoc(), i64, n);
          LLVM::MemmoveOp::create(b, st.getLoc(), st.getAddr(), ld.getAddr(), len,
                                  /*isVolatile=*/false);
          st.erase();
        } else {
          // Load just the element, where the aggregate load was.
          auto ev = cast<LLVM::ExtractValueOp>(user);
          b.setInsertionPoint(ld);
          SmallVector<LLVM::GEPArg> idx{0};
          for (int64_t p : ev.getPosition())
            idx.push_back(static_cast<int32_t>(p));
          Value addr = LLVM::GEPOp::create(b, ev.getLoc(), ptrTy, t, ld.getAddr(), idx);
          Value v = LLVM::LoadOp::create(b, ev.getLoc(), ev.getType(), addr);
          ev.replaceAllUsesWith(v);
          ev.erase();
        }
      }
      ld.erase();
    }

    // Whatever large aggregate value is left would hang code generation.
    // Global initializer regions become constant data, not code: skip them.
    WalkResult r = m.walk<WalkOrder::PreOrder>([&](Operation *op) {
      if (isa<LLVM::GlobalOp>(op))
        return WalkResult::skip();
      auto tooBig = [&](Type t) { return size(t) > maxValueSize; };
      for (Type t : op->getResultTypes())
        if (tooBig(t)) {
          op->emitError("aggregate value of ")
              << size(t) << " bytes (type " << t
              << ") is too large to be an SSA value; copy it through memory "
                 "(llvm.intr.memmove) instead";
          return WalkResult::interrupt();
        }
      if (auto fn = dyn_cast<LLVM::LLVMFuncOp>(op)) {
        for (Type t : fn.getFunctionType().getParams())
          if (tooBig(t)) {
            op->emitError("parameter of ") << size(t)
                                           << " bytes is too large; pass it by pointer";
            return WalkResult::interrupt();
          }
      }
      return WalkResult::advance();
    });
    if (r.wasInterrupted())
      signalPassFailure();
  }
};

} // namespace

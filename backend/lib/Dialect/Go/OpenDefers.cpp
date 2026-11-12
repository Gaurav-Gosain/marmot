// OpenDefers.cpp: marks go.defer_frame `norecover` when no deferred call of
// the frame can recover a panic.
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/SymbolTable.h"

namespace marmot::go {
#define GEN_PASS_DEF_GOOPENDEFERS
#include "marmot/Dialect/Go/Passes.h.inc"
} // namespace marmot::go

using namespace mlir;
using namespace marmot::go;

namespace {

struct GoOpenDefersPass : marmot::go::impl::GoOpenDefersBase<GoOpenDefersPass> {
  void runOnOperation() override {
    ModuleOp m = getOperation();
    SymbolTable symbols(m);
    llvm::DenseMap<Operation *, bool> memo;

    // R1: a defined function whose body has no go.recover and no call to
    // runtime.gorecover. Declarations are unknown, so they may recover.
    auto mayRecover = [&](LLVM::LLVMFuncOp fn) -> bool {
      if (!fn || fn.isExternal())
        return true;
      auto it = memo.find(fn);
      if (it != memo.end())
        return it->second;
      bool r = fn.walk([](Operation *op) {
                   if (isa<RecoverOp>(op))
                     return WalkResult::interrupt();
                   if (auto c = dyn_cast<LLVM::CallOp>(op))
                     if (c.getCallee() && *c.getCallee() == "runtime.gorecover")
                       return WalkResult::interrupt();
                   return WalkResult::advance();
                 }).wasInterrupted();
      memo[fn] = r;
      return r;
    };

    // R2: a deferred func value that cannot recover.
    auto deferMayRecover = [&](DeferOp d) -> bool {
      auto mk = d.getFn().getDefiningOp<MakeClosureOp>();
      if (!mk)
        return true;
      auto code = symbols.lookup<LLVM::LLVMFuncOp>(mk.getFn());
      if (mayRecover(code))
        return true;
      if (!d.getWrapped())
        return false;
      // A wrapper's direct callee is also allowed to recover.
      bool r = false;
      code.walk([&](LLVM::CallOp c) {
        if (r)
          return;
        auto callee = c.getCallee();
        r = !callee || mayRecover(symbols.lookup<LLVM::LLVMFuncOp>(*callee));
      });
      return r;
    };

    m.walk([&](DeferFrameOp frame) {
      auto fn = frame->getParentOfType<LLVM::LLVMFuncOp>();
      bool any = false;
      fn.walk([&](DeferOp d) { any = any || deferMayRecover(d); });
      if (!any)
        frame.setNorecover(true);
    });
  }
};

} // namespace

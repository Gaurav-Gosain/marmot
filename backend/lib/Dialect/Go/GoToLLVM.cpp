// GoToLLVM.cpp: lowers go dialect ops to the llvm dialect plus runtime calls.
//
// The output is exactly the Phase A form documented per op in
// docs/dialect.md. All values already have LLVM types, so no type conversion
// is needed: one walk over the module rewrites every go op in place. This is
// much cheaper than the dialect conversion framework.
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/Builders.h"
#include "mlir/IR/BuiltinOps.h"
#include "mlir/IR/PatternMatch.h"
#include "mlir/IR/SymbolTable.h"
#include "llvm/ADT/TypeSwitch.h"

namespace marmot::go {
#define GEN_PASS_DEF_GOTOLLVM
#include "marmot/Dialect/Go/Passes.h.inc"
} // namespace marmot::go

using namespace mlir;
using namespace marmot::go;

namespace {

class Lowering {
public:
  explicit Lowering(ModuleOp module)
      : module(module), symbols(module), b(module.getContext()),
        ptrTy(LLVM::LLVMPointerType::get(module.getContext())) {}

  LogicalResult run() {
    WalkResult r = module.walk([&](Operation *op) {
      if (op->getDialect() == nullptr ||
          op->getDialect()->getNamespace() != GoDialect::getDialectNamespace())
        return WalkResult::advance();
      b.setInsertionPoint(op);
      LogicalResult res = TypeSwitch<Operation *, LogicalResult>(op)
                              .Case([&](NewOp o) { return lower(o); })
                              .Case([&](MakeSliceOp o) { return lower(o); })
                              .Case([&](MakeClosureOp o) { return lower(o); })
                              .Case([&](BoxOp o) { return lower(o); })
                              .Default([&](Operation *o) {
                                return o->emitOpError("no lowering to llvm");
                              });
      return failed(res) ? WalkResult::interrupt() : WalkResult::advance();
    });
    return failure(r.wasInterrupted());
  }

private:
  // Returns the runtime function `runtime.<name>` with the given LLVM type,
  // declaring it if the module does not have it.
  FailureOr<LLVM::LLVMFuncOp> runtimeFunc(Operation *at, StringRef name,
                                          Type result, ArrayRef<Type> params) {
    std::string sym = ("runtime." + name).str();
    auto fnTy = LLVM::LLVMFunctionType::get(
        result ? result : LLVM::LLVMVoidType::get(module.getContext()), params);
    if (Operation *existing = symbols.lookup(sym)) {
      auto fn = dyn_cast<LLVM::LLVMFuncOp>(existing);
      if (!fn || fn.getFunctionType() != fnTy) {
        at->emitOpError("runtime function @\"")
            << sym << "\" exists with a different type; want " << fnTy;
        return failure();
      }
      return fn;
    }
    OpBuilder::InsertionGuard g(b);
    b.setInsertionPointToEnd(module.getBody());
    auto fn = LLVM::LLVMFuncOp::create(b, module.getLoc(), sym, fnTy);
    symbols.insert(fn);
    return fn;
  }

  FailureOr<Value> addressOf(Operation *at, FlatSymbolRefAttr sym) {
    Operation *target = symbols.lookup(sym.getAttr());
    if (!target || !isa<LLVM::GlobalOp, LLVM::LLVMFuncOp>(target)) {
      at->emitOpError("symbol ") << sym << " is not an llvm global or function";
      return failure();
    }
    return LLVM::AddressOfOp::create(b, at->getLoc(), ptrTy, sym.getValue())
        .getResult();
  }

  Value call(Location loc, LLVM::LLVMFuncOp fn, ValueRange args) {
    auto c = LLVM::CallOp::create(b, loc, fn, args);
    return c.getNumResults() ? c.getResult() : Value();
  }

  // runtime.newobject(type) -> ptr
  FailureOr<Value> newObject(Operation *at, FlatSymbolRefAttr type) {
    auto t = addressOf(at, type);
    if (failed(t))
      return failure();
    auto fn = runtimeFunc(at, "newobject", ptrTy, {ptrTy});
    if (failed(fn))
      return failure();
    return call(at->getLoc(), *fn, {*t});
  }

  void storeMem(Location loc, Value v, Value addr) {
    Type mt = memoryType(v.getType());
    if (mt != v.getType())
      v = LLVM::ZExtOp::create(b, loc, mt, v);
    LLVM::StoreOp::create(b, loc, v, addr);
  }

  LogicalResult lower(NewOp op) {
    auto p = newObject(op, op.getDescAttr());
    if (failed(p))
      return failure();
    op.replaceAllUsesWith(*p);
    op.erase();
    return success();
  }

  LogicalResult lower(MakeSliceOp op) {
    Location loc = op.getLoc();
    auto t = addressOf(op, op.getDescAttr());
    if (failed(t))
      return failure();
    Type intTy = op.getLen().getType();
    auto fn = runtimeFunc(op, "makeslice", ptrTy, {ptrTy, intTy, intTy});
    if (failed(fn))
      return failure();
    Value p = call(loc, *fn, {*t, op.getLen(), op.getCap()});
    Type sliceTy = op.getType();
    Value s = LLVM::PoisonOp::create(b, loc, sliceTy);
    s = LLVM::InsertValueOp::create(b, loc, s, p, ArrayRef<int64_t>{0});
    s = LLVM::InsertValueOp::create(b, loc, s, op.getLen(), ArrayRef<int64_t>{1});
    s = LLVM::InsertValueOp::create(b, loc, s, op.getCap(), ArrayRef<int64_t>{2});
    op.replaceAllUsesWith(s);
    op.erase();
    return success();
  }

  LogicalResult lower(MakeClosureOp op) {
    Location loc = op.getLoc();
    auto p = newObject(op, op.getDescAttr());
    if (failed(p))
      return failure();
    auto code = addressOf(op, op.getFnAttr());
    if (failed(code))
      return failure();
    LLVM::StoreOp::create(b, loc, *code, *p);
    LLVM::LLVMStructType env = op.getEnvType();
    for (auto [i, v] : llvm::enumerate(op.getCaptures())) {
      Value addr = LLVM::GEPOp::create(
          b, loc, ptrTy, env, *p,
          ArrayRef<LLVM::GEPArg>{0, static_cast<int32_t>(i + 1)});
      storeMem(loc, v, addr);
    }
    op.replaceAllUsesWith(*p);
    op.erase();
    return success();
  }

  LogicalResult lower(BoxOp op) {
    auto p = newObject(op, op.getDescAttr());
    if (failed(p))
      return failure();
    storeMem(op.getLoc(), op.getValue(), *p);
    op.replaceAllUsesWith(*p);
    op.erase();
    return success();
  }

  ModuleOp module;
  SymbolTable symbols;
  OpBuilder b;
  Type ptrTy;
};

struct GoToLLVMPass : marmot::go::impl::GoToLLVMBase<GoToLLVMPass> {
  void runOnOperation() override {
    if (failed(Lowering(getOperation()).run()))
      signalPassFailure();
  }
};

} // namespace

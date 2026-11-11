// GoToLLVM.cpp: lowers go dialect ops to the llvm dialect plus runtime calls.
//
// The output is exactly the Phase A form documented per op in
// docs/dialect.md. All values already have LLVM types, so no type conversion
// is needed: one walk collects the go ops, then each is rewritten in place.
// This is much cheaper than the dialect conversion framework.
//
// Checks that can panic split the block: the check branches to the rest of
// the block or to a panic block. Each function gets one panic block per
// runtime panic function and argument types; it takes the panic arguments as
// block arguments, calls the noreturn runtime function, and is unreachable
// after. The branch is weighted as never taken.
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/Builders.h"
#include "mlir/IR/BuiltinOps.h"
#include "mlir/IR/PatternMatch.h"
#include "mlir/IR/SymbolTable.h"
#include "llvm/ADT/TypeSwitch.h"

#include <map>

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
    SmallVector<Operation *> ops;
    Dialect *goDialect = module.getContext()->getLoadedDialect<GoDialect>();
    if (!goDialect)
      return success();
    module.walk([&](Operation *op) {
      if (op->getDialect() == goDialect)
        ops.push_back(op);
    });
    for (Operation *op : ops) {
      b.setInsertionPoint(op);
      LogicalResult res = TypeSwitch<Operation *, LogicalResult>(op)
                              .Case([&](NewOp o) { return lower(o); })
                              .Case([&](MakeSliceOp o) { return lower(o); })
                              .Case([&](MakeClosureOp o) { return lower(o); })
                              .Case([&](BoxOp o) { return lower(o); })
                              .Case([&](BoundsCheckOp o) { return lower(o); })
                              .Case([&](NilCheckOp o) { return lower(o); })
                              .Case([&](IndexOp o) { return lower(o); })
                              .Case([&](SliceOp o) { return lower(o); })
                              .Case([&](MakeIfaceOp o) { return lower(o); })
                              .Case([&](IfaceCallOp o) { return lower(o); })
                              .Case([&](TypeAssertOp o) { return lower(o); })
                              .Default([&](Operation *o) {
                                return o->emitOpError("no lowering to llvm");
                              });
      if (failed(res))
        return failure();
    }
    return success();
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

  // Declares a noreturn, cold runtime panic function.
  FailureOr<LLVM::LLVMFuncOp> panicFunc(Operation *at, StringRef name,
                                        ArrayRef<Type> params) {
    bool existed = symbols.lookup(("runtime." + name).str()) != nullptr;
    auto fn = runtimeFunc(at, name, Type(), params);
    if (failed(fn) || existed)
      return fn;
    fn->setPassthroughAttr(b.getArrayAttr(
        {b.getStringAttr("noreturn"), b.getStringAttr("cold"),
         b.getStringAttr("nounwind")}));
    return fn;
  }

  // The shared panic block of the function containing at, for runtime
  // function name with the given argument types.
  FailureOr<Block *> panicBlock(Operation *at, StringRef name,
                                ArrayRef<Type> params) {
    Region *body = &at->getParentOfType<LLVM::LLVMFuncOp>().getBody();
    std::string key = name.str();
    for (Type t : params) {
      llvm::raw_string_ostream os(key);
      os << "," << t;
    }
    auto &slot = panicBlocks[{body, key}];
    if (slot)
      return slot;
    auto fn = panicFunc(at, name, params);
    if (failed(fn))
      return failure();
    OpBuilder::InsertionGuard g(b);
    Block *blk = b.createBlock(body, body->end());
    SmallVector<Location> locs(params.size(), at->getLoc());
    for (auto [t, l] : llvm::zip(params, locs))
      blk->addArgument(t, l);
    LLVM::CallOp::create(b, at->getLoc(), *fn, blk->getArguments());
    LLVM::UnreachableOp::create(b, at->getLoc());
    slot = blk;
    return blk;
  }

  // Splits the block before at. If ok is false, control goes to the panic
  // block of runtime function name with args; otherwise it continues at at.
  // Leaves the insertion point at at.
  LogicalResult emitCheck(Operation *at, Value ok, StringRef name,
                          ValueRange args) {
    SmallVector<Type> types(args.getTypes());
    auto pb = panicBlock(at, name, types);
    if (failed(pb))
      return failure();
    Block *before = at->getBlock();
    Block *cont = before->splitBlock(at);
    b.setInsertionPointToEnd(before);
    LLVM::CondBrOp::create(b, at->getLoc(), ok, cont, ValueRange(), *pb, args,
                           std::make_pair(uint32_t(1) << 20, uint32_t(1)));
    b.setInsertionPoint(at);
    return success();
  }

  Value icmp(Location loc, LLVM::ICmpPredicate p, Value x, Value y) {
    return LLVM::ICmpOp::create(b, loc, p, x, y);
  }

  LogicalResult lower(BoundsCheckOp op) {
    Value ok = icmp(op.getLoc(), LLVM::ICmpPredicate::ult, op.getIndex(), op.getLen());
    if (failed(emitCheck(op, ok, "panicindex", {op.getIndex(), op.getLen()})))
      return failure();
    op.erase();
    return success();
  }

  LogicalResult lower(NilCheckOp op) {
    Location loc = op.getLoc();
    Value null = LLVM::ZeroOp::create(b, loc, ptrTy);
    Value ok = icmp(loc, LLVM::ICmpPredicate::ne, op.getPtr(), null);
    if (failed(emitCheck(op, ok, "panicnil", {})))
      return failure();
    op.erase();
    return success();
  }

  LogicalResult lower(IndexOp op) {
    Location loc = op.getLoc();
    Value ok = icmp(loc, LLVM::ICmpPredicate::ult, op.getIndex(), op.getLen());
    if (failed(emitCheck(op, ok, "panicindex", {op.getIndex(), op.getLen()})))
      return failure();
    Value addr = LLVM::GEPOp::create(b, loc, ptrTy, op.getElemType(), op.getBase(),
                                     ValueRange{op.getIndex()});
    op.replaceAllUsesWith(addr);
    op.erase();
    return success();
  }

  LogicalResult lower(SliceOp op) {
    Location loc = op.getLoc();
    using P = LLVM::ICmpPredicate;
    Value lo = op.getLo(), hi = op.getHi(), cap = op.getCap();
    Value max = op.getMax();
    // lo <= hi <= max <= cap, all unsigned (a negative index is huge).
    Value ok = icmp(loc, P::ule, lo, hi);
    Value top = max ? max : cap;
    ok = LLVM::AndOp::create(b, loc, ok, icmp(loc, P::ule, hi, top));
    if (max)
      ok = LLVM::AndOp::create(b, loc, ok, icmp(loc, P::ule, max, cap));
    if (failed(emitCheck(op, ok, "panicslice", {lo, hi, cap})))
      return failure();
    Value len = LLVM::SubOp::create(b, loc, hi, lo);
    Value newCap = max ? LLVM::SubOp::create(b, loc, max, lo).getResult() : len;
    // Do not form a pointer past the end of the object: an empty result keeps
    // the base pointer (as gc does).
    Value moved = LLVM::GEPOp::create(b, loc, ptrTy, op.getElemType(), op.getBase(),
                                      ValueRange{lo});
    Value zero = LLVM::ConstantOp::create(b, loc, lo.getType(), 0);
    Value empty = icmp(loc, P::eq, newCap, zero);
    Value ptr = LLVM::SelectOp::create(b, loc, empty, op.getBase(), moved);
    Type resTy = op.getType();
    Value s = LLVM::PoisonOp::create(b, loc, resTy);
    s = LLVM::InsertValueOp::create(b, loc, s, ptr, ArrayRef<int64_t>{0});
    s = LLVM::InsertValueOp::create(b, loc, s, len, ArrayRef<int64_t>{1});
    if (max)
      s = LLVM::InsertValueOp::create(b, loc, s, newCap, ArrayRef<int64_t>{2});
    op.replaceAllUsesWith(s);
    op.erase();
    return success();
  }

  // Splits the block before at and returns a value that is thenFn() when
  // cond holds and elseVal otherwise. thenFn runs in a new block. Leaves the
  // insertion point at at.
  Value condValue(Operation *at, Value cond, function_ref<Value()> thenFn,
                  Value elseVal) {
    Location loc = at->getLoc();
    Block *before = at->getBlock();
    Block *cont = before->splitBlock(at);
    Value result = cont->addArgument(elseVal.getType(), loc);
    Block *thenBlk = new Block();
    before->getParent()->getBlocks().insert(cont->getIterator(), thenBlk);
    b.setInsertionPointToEnd(before);
    LLVM::CondBrOp::create(b, loc, cond, thenBlk, ValueRange(), cont,
                           ValueRange{elseVal});
    b.setInsertionPointToEnd(thenBlk);
    Value v = thenFn();
    LLVM::BrOp::create(b, loc, ValueRange{v}, cont);
    b.setInsertionPoint(at);
    return result;
  }

  Value loadInvariant(Location loc, Type t, Value addr) {
    return LLVM::LoadOp::create(b, loc, t, addr, /*alignment=*/0,
                                /*isVolatile=*/false, /*isNonTemporal=*/false,
                                /*isInvariant=*/true);
  }

  // The dynamic type of interface value x: word 0 for an empty interface;
  // for a non-empty one, itab.typ (word 1 of the itab) or null for a nil tab.
  Value dynamicType(Operation *at, Value x, bool fromIface) {
    Location loc = at->getLoc();
    Value w0 = LLVM::ExtractValueOp::create(b, loc, x, ArrayRef<int64_t>{0});
    if (!fromIface)
      return w0;
    Value null = LLVM::ZeroOp::create(b, loc, ptrTy);
    Value nonNil = icmp(loc, LLVM::ICmpPredicate::ne, w0, null);
    return condValue(at, nonNil, [&] {
      Value slot = LLVM::GEPOp::create(b, loc, ptrTy, ptrTy, w0,
                                       ArrayRef<LLVM::GEPArg>{1});
      return loadInvariant(loc, ptrTy, slot);
    }, null);
  }

  Value makeIface(Location loc, Value tab, Value data) {
    Value s = LLVM::PoisonOp::create(b, loc, ifaceType(b.getContext()));
    s = LLVM::InsertValueOp::create(b, loc, s, tab, ArrayRef<int64_t>{0});
    return LLVM::InsertValueOp::create(b, loc, s, data, ArrayRef<int64_t>{1});
  }

  LogicalResult lower(MakeIfaceOp op) {
    auto tab = addressOf(op, op.getTabAttr());
    if (failed(tab))
      return failure();
    op.replaceAllUsesWith(makeIface(op.getLoc(), *tab, op.getData()));
    op.erase();
    return success();
  }

  LogicalResult lower(IfaceCallOp op) {
    Location loc = op.getLoc();
    Value tab = LLVM::ExtractValueOp::create(b, loc, op.getIface(), ArrayRef<int64_t>{0});
    Value data = LLVM::ExtractValueOp::create(b, loc, op.getIface(), ArrayRef<int64_t>{1});
    Value null = LLVM::ZeroOp::create(b, loc, ptrTy);
    Value ok = icmp(loc, LLVM::ICmpPredicate::ne, tab, null);
    if (failed(emitCheck(op, ok, "panicnil", {})))
      return failure();
    // itab = {inter, typ, fun[N]}: method i is word i+2.
    Value slot = LLVM::GEPOp::create(
        b, loc, ptrTy, ptrTy, tab,
        ArrayRef<LLVM::GEPArg>{static_cast<int32_t>(op.getMethod() + 2)});
    Value fn = loadInvariant(loc, ptrTy, slot);
    SmallVector<Type> params{ptrTy};
    llvm::append_range(params, op.getArgs().getTypes());
    Type res = op.getNumResults() ? op.getResult(0).getType()
                                  : LLVM::LLVMVoidType::get(b.getContext());
    auto fnTy = LLVM::LLVMFunctionType::get(res, params);
    SmallVector<Value> args{fn, data};
    llvm::append_range(args, op.getArgs());
    auto call = LLVM::CallOp::create(b, loc, fnTy, args);
    op.replaceAllUsesWith(call.getResults());
    op.erase();
    return success();
  }

  LogicalResult lower(TypeAssertOp op) {
    Location loc = op.getLoc();
    using P = LLVM::ICmpPredicate;
    auto want = addressOf(op, op.getDescAttr());
    auto src = addressOf(op, op.getSrcAttr());
    if (failed(want) || failed(src))
      return failure();
    Value data = LLVM::ExtractValueOp::create(b, loc, op.getX(), ArrayRef<int64_t>{1});
    Value have = dynamicType(op, op.getX(), op.getFromIface());
    Value null = LLVM::ZeroOp::create(b, loc, ptrTy);
    Value result, ok;
    if (!op.getToIface() && !op.getToAny()) {
      // Concrete target: type identity is pointer identity.
      ok = icmp(loc, P::eq, have, *want);
      if (op.getCommaok()) {
        result = LLVM::SelectOp::create(b, loc, ok, data, null);
      } else {
        if (failed(emitCheck(op, ok, "panicTypeAssert", {have, *want, *src})))
          return failure();
        result = data;
      }
    } else if (op.getToAny()) {
      ok = icmp(loc, P::ne, have, null);
      if (!op.getCommaok() &&
          failed(emitCheck(op, ok, "panicTypeAssert", {have, *want, *src})))
        return failure();
      result = makeIface(loc, have, data);
    } else {
      Value nonNil = icmp(loc, P::ne, have, null);
      if (op.getCommaok()) {
        auto fn = runtimeFunc(op, "assertE2I2", ptrTy, {ptrTy, ptrTy});
        if (failed(fn))
          return failure();
        Value tab = condValue(op, nonNil, [&] {
          return call(loc, *fn, {*want, have});
        }, null);
        ok = icmp(loc, P::ne, tab, null);
        result = makeIface(loc, tab, LLVM::SelectOp::create(b, loc, ok, data, null));
      } else {
        if (failed(emitCheck(op, nonNil, "panicTypeAssert", {have, *want, *src})))
          return failure();
        auto fn = runtimeFunc(op, "assertE2I", ptrTy, {ptrTy, ptrTy});
        if (failed(fn))
          return failure();
        result = makeIface(loc, call(loc, *fn, {*want, have}), data);
      }
    }
    op.getResult().replaceAllUsesWith(result);
    if (op.getOk())
      op.getOk().replaceAllUsesWith(ok);
    op.erase();
    return success();
  }

  ModuleOp module;
  SymbolTable symbols;
  std::map<std::pair<Region *, std::string>, Block *> panicBlocks;
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

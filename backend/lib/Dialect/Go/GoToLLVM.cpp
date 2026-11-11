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
#include "mlir/IR/Matchers.h"
#include "mlir/IR/PatternMatch.h"
#include "mlir/IR/SymbolTable.h"
#include "mlir/IR/RegionGraphTraits.h"
#include "mlir/Interfaces/DataLayoutInterfaces.h"
#include "llvm/ADT/DenseSet.h"
#include "llvm/ADT/SCCIterator.h"
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
    // Escape summaries become LLVM no-capture attributes (the summary rules
    // are strict enough for that promise; see Escape.cpp).
    module.walk([&](LLVM::LLVMFuncOp fn) {
      for (unsigned i = 0; i < fn.getNumArguments(); ++i)
        if (fn.getArgAttr(i, "go.noescape")) {
          fn.removeArgAttr(i, "go.noescape");
          fn.setArgAttr(i, LLVM::LLVMDialect::getNoCaptureAttrName(),
                        UnitAttr::get(fn.getContext()));
        }
    });
    SmallVector<Operation *> ops;
    Dialect *goDialect = module.getContext()->getLoadedDialect<GoDialect>();
    if (!goDialect)
      return success();
    module.walk([&](Operation *op) {
      if (op->getDialect() == goDialect)
        ops.push_back(op);
    });
    // Defer frames first: go.defer and go.run_defers need the frame.
    std::stable_partition(ops.begin(), ops.end(),
                          [](Operation *op) { return isa<DeferFrameOp>(op); });
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
                              .Case([&](DeferFrameOp o) { return lower(o); })
                              .Case([&](DeferOp o) { return lower(o); })
                              .Case([&](RunDefersOp o) { return lower(o); })
                              .Case([&](GoOp o) { return lower(o); })
                              .Case([&](PanicOp o) { return lower(o); })
                              .Case([&](RecoverOp o) { return lower(o); })
                              .Case([&](StrConcatOp o) { return lower(o); })
                              .Case([&](StrToBytesOp o) { return lower(o); })
                              .Case([&](BytesToStrOp o) { return lower(o); })
                              .Case([&](MakeMapOp o) { return lower(o); })
                              .Case([&](MapLookupOp o) { return lower(o); })
                              .Case([&](MapUpdateOp o) { return lower(o); })
                              .Case([&](MapDeleteOp o) { return lower(o); })
                              .Case([&](MakeChanOp o) { return lower(o); })
                              .Case([&](ChanSendOp o) { return lower(o); })
                              .Case([&](ChanRecvOp o) { return lower(o); })
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
    return declareFunc(at, ("runtime." + name).str(), result, params);
  }

  // Returns function sym with the given LLVM type, declaring it if needed.
  FailureOr<LLVM::LLVMFuncOp> declareFunc(Operation *at, const std::string &sym,
                                          Type result, ArrayRef<Type> params) {
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

  // A stack slot for a non-escaping allocation: an entry block alloca, zeroed
  // where the original op ran (so each execution starts from zero).
  Value stackSlot(Operation *at, Type t) {
    Value p = entryAlloca(at->getParentOfType<LLVM::LLVMFuncOp>(), t);
    Value zero = LLVM::ZeroOp::create(b, at->getLoc(), t);
    LLVM::StoreOp::create(b, at->getLoc(), zero, p);
    return p;
  }

  LogicalResult lower(NewOp op) {
    if (op.getStack()) {
      op.replaceAllUsesWith(stackSlot(op, op.getElemType()));
      op.erase();
      return success();
    }
    auto p = newObject(op, op.getDescAttr());
    if (failed(p))
      return failure();
    op.replaceAllUsesWith(*p);
    op.erase();
    return success();
  }

  LogicalResult lower(MakeSliceOp op) {
    Location loc = op.getLoc();
    Type intTy = op.getLen().getType();
    Value p;
    if (op.getStack()) {
      APInt cap;
      (void)matchPattern(op.getCap(), m_ConstantInt(&cap));
      p = stackSlot(op, LLVM::LLVMArrayType::get(op.getElemType(), cap.getZExtValue()));
    } else {
      auto t = addressOf(op, op.getDescAttr());
      if (failed(t))
        return failure();
      auto fn = runtimeFunc(op, "makeslice", ptrTy, {ptrTy, intTy, intTy});
      if (failed(fn))
        return failure();
      p = call(loc, *fn, {*t, op.getLen(), op.getCap()});
    }
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
    FailureOr<Value> p = op.getStack() ? FailureOr<Value>(stackSlot(op, op.getEnvType()))
                                       : newObject(op, op.getDescAttr());
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
    FailureOr<Value> p =
        op.getStack()
            ? FailureOr<Value>(entryAlloca(op->getParentOfType<LLVM::LLVMFuncOp>(),
                                           memoryType(op.getValue().getType())))
            : newObject(op, op.getDescAttr());
    if (failed(p))
      return failure();
    storeMem(op.getLoc(), op.getValue(), *p);
    op.replaceAllUsesWith(*p);
    op.erase();
    return success();
  }

  // Adds a passthrough attribute (like "noreturn") to fn if missing.
  void addPassthrough(LLVM::LLVMFuncOp fn, StringRef attr) {
    SmallVector<Attribute> attrs;
    if (ArrayAttr old = fn.getPassthroughAttr()) {
      for (Attribute a : old)
        if (auto sa = dyn_cast<StringAttr>(a); sa && sa.getValue() == attr)
          return;
      attrs.append(old.begin(), old.end());
    }
    attrs.push_back(b.getStringAttr(attr));
    fn.setPassthroughAttr(b.getArrayAttr(attrs));
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

  // Codes of runtime.panicBounds (lib/src/runtime/error.go).
  enum BoundsCode : int64_t {
    boundsIndex = 0,
    boundsSliceAlen = 1,
    boundsSliceAcap = 2,
    boundsSliceB = 3,
    boundsSlice3Alen = 4,
    boundsSlice3Acap = 5,
    boundsSlice3B = 6,
    boundsSlice3C = 7,
  };

  // Emits: if !(x <= y) (or x < y when strict) panicBounds(code, x, signed, y).
  // x and y are extended or truncated to i64 for the call.
  LogicalResult boundsCheck(Operation *at, Value x, Value y, bool strict,
                            BoundsCode code, bool isUnsigned) {
    Location loc = at->getLoc();
    Value ok = icmp(loc, strict ? LLVM::ICmpPredicate::ult : LLVM::ICmpPredicate::ule,
                    x, y);
    auto toI64 = [&](Value v, bool sext) -> Value {
      unsigned w = cast<IntegerType>(v.getType()).getWidth();
      if (w == 64)
        return v;
      if (w > 64)
        return LLVM::TruncOp::create(b, loc, i64(), v);
      return sext ? LLVM::SExtOp::create(b, loc, i64(), v).getResult()
                  : LLVM::ZExtOp::create(b, loc, i64(), v).getResult();
    };
    Value cx = toI64(x, !isUnsigned), cy = toI64(y, true);
    Value c = LLVM::ConstantOp::create(b, loc, i64(), code);
    Value sg = LLVM::ConstantOp::create(b, loc, b.getI1Type(), isUnsigned ? 0 : 1);
    return emitCheck(at, ok, "panicBounds", {c, cx, sg, cy});
  }

  LogicalResult lower(BoundsCheckOp op) {
    if (failed(boundsCheck(op, op.getIndex(), op.getLen(), true, boundsIndex,
                           op.getUnsignedIndex())))
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
    if (failed(boundsCheck(op, op.getIndex(), op.getLen(), true, boundsIndex,
                           op.getUnsignedIndex())))
      return failure();
    Value addr = LLVM::GEPOp::create(b, loc, ptrTy, op.getElemType(), op.getBase(),
                                     ValueRange{op.getIndex()});
    op.replaceAllUsesWith(addr);
    op.erase();
    return success();
  }

  // Checks in gc order (lib/src/runtime/error.go), first failure reported.
  LogicalResult lower(SliceOp op) {
    Location loc = op.getLoc();
    Value lo = op.getLo(), hi = op.getHi(), cap = op.getCap(), max = op.getMax();
    bool uns = op.getUnsignedIndex();
    bool isString = cast<LLVM::LLVMStructType>(op.getType()).getBody().size() == 2;
    bool lenBound = isString || op.getLenBound();
    if (max) {
      if (failed(boundsCheck(op, max, cap, false,
                             lenBound ? boundsSlice3Alen : boundsSlice3Acap, uns)) ||
          failed(boundsCheck(op, hi, max, false, boundsSlice3B, uns)) ||
          failed(boundsCheck(op, lo, hi, false, boundsSlice3C, uns)))
        return failure();
    } else {
      if (failed(boundsCheck(op, hi, cap, false,
                             lenBound ? boundsSliceAlen : boundsSliceAcap, uns)) ||
          failed(boundsCheck(op, lo, hi, false, boundsSliceB, uns)))
        return failure();
    }
    Value len = LLVM::SubOp::create(b, loc, hi, lo);
    Value newCap = isString ? len : LLVM::SubOp::create(b, loc, max ? max : cap, lo).getResult();
    // Do not form a pointer past the end of the object: an empty result keeps
    // the base pointer (as gc does).
    Value moved = LLVM::GEPOp::create(b, loc, ptrTy, op.getElemType(), op.getBase(),
                                      ValueRange{lo});
    Value zero = LLVM::ConstantOp::create(b, loc, lo.getType(), 0);
    Value empty = icmp(loc, LLVM::ICmpPredicate::eq, newCap, zero);
    Value ptr = LLVM::SelectOp::create(b, loc, empty, op.getBase(), moved);
    Type resTy = op.getType();
    Value s = LLVM::PoisonOp::create(b, loc, resTy);
    s = LLVM::InsertValueOp::create(b, loc, s, ptr, ArrayRef<int64_t>{0});
    s = LLVM::InsertValueOp::create(b, loc, s, len, ArrayRef<int64_t>{1});
    if (!isString)
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

  //===--------------------------------------------------------------------===//
  // Control family
  //===--------------------------------------------------------------------===//

  static constexpr unsigned kJmpbufWords = 32; // RT_JMPBUF_WORDS in rt.h

  Type i64() { return b.getI64Type(); }
  Type i32() { return b.getI32Type(); }

  // The _defer record: {link *_defer, fn func(), frame *_deferframe, wrapped bool}.
  LLVM::LLVMStructType deferRecordType() {
    return LLVM::LLVMStructType::getLiteral(
        b.getContext(), {ptrTy, ptrTy, ptrTy, b.getI8Type()});
  }

  // Creates `alloca 1 x t` at the start of the entry block of fn.
  Value entryAlloca(LLVM::LLVMFuncOp fn, Type t) {
    OpBuilder::InsertionGuard g(b);
    b.setInsertionPointToStart(&fn.getBody().front());
    Value one = LLVM::ConstantOp::create(b, fn.getLoc(), i64(), 1);
    return LLVM::AllocaOp::create(b, fn.getLoc(), ptrTy, t, one);
  }

  // True if block blk of fn is on a CFG cycle.
  bool inCycle(LLVM::LLVMFuncOp fn, Block *blk) {
    Region *r = &fn.getBody();
    auto it = cyclic.find(r);
    if (it == cyclic.end()) {
      llvm::DenseSet<Block *> set;
      for (auto scc = llvm::scc_begin(r); !scc.isAtEnd(); ++scc) {
        if (scc->size() > 1 || scc.hasCycle())
          for (Block *bb : *scc)
            set.insert(bb);
      }
      it = cyclic.try_emplace(r, std::move(set)).first;
    }
    return it->second.contains(blk);
  }

  // Marks the loads in blk from allocas, and every store to those allocas,
  // volatile. After rt_setjmp returns a second time, values in registers are
  // stale; volatile keeps the named results in memory so the Recover block
  // sees the values stored before the panic.
  void pinRecoverLoads(Block *blk) {
    llvm::SmallPtrSet<Operation *, 8> allocas;
    for (Operation &op : *blk)
      if (auto ld = dyn_cast<LLVM::LoadOp>(op))
        if (auto a = ld.getAddr().getDefiningOp<LLVM::AllocaOp>())
          allocas.insert(a);
    for (Operation *a : allocas) {
      SmallVector<Value> work{a->getResult(0)};
      while (!work.empty()) {
        Value v = work.pop_back_val();
        for (Operation *user : v.getUsers()) {
          if (auto ld = dyn_cast<LLVM::LoadOp>(user))
            ld.setVolatile_(true);
          else if (auto st = dyn_cast<LLVM::StoreOp>(user); st && st.getAddr() == v)
            st.setVolatile_(true);
          else if (auto gep = dyn_cast<LLVM::GEPOp>(user))
            work.push_back(gep.getResult());
        }
      }
    }
  }

  LogicalResult lower(DeferFrameOp op) {
    Location loc = op.getLoc();
    auto fn = op->getParentOfType<LLVM::LLVMFuncOp>();
    if (frames.count(&fn.getBody()))
      return op.emitOpError("more than one go.defer_frame in a function");
    auto frameTy = LLVM::LLVMArrayType::get(i64(), kJmpbufWords);
    Value fr = entryAlloca(fn, frameTy);
    frames[&fn.getBody()] = fr;
    auto setjmp = declareFunc(op, "rt_setjmp", i32(), {ptrTy});
    auto deferreturn = runtimeFunc(op, "deferreturn", Type(), {ptrTy});
    if (failed(setjmp) || failed(deferreturn))
      return failure();
    addPassthrough(*setjmp, "returns_twice");
    Block *normal = op.getNormal(), *recover = op.getRecover();
    pinRecoverLoads(recover);
    Value r = call(loc, *setjmp, {fr});
    Value zero = LLVM::ConstantOp::create(b, loc, i32(), 0);
    Value recovered = icmp(loc, LLVM::ICmpPredicate::ne, r, zero);
    // A recovered panic runs the rest of this frame's defers, then goes to
    // the Recover block.
    Block *pad = new Block();
    fn.getBody().getBlocks().insert(std::next(op->getBlock()->getIterator()), pad);
    LLVM::CondBrOp::create(b, loc, recovered, pad, ValueRange(), normal,
                           ValueRange(), std::make_pair(uint32_t(1), uint32_t(1) << 20));
    b.setInsertionPointToEnd(pad);
    call(loc, *deferreturn, {fr});
    LLVM::BrOp::create(b, loc, ValueRange(), recover);
    op.erase();
    return success();
  }

  FailureOr<Value> frameOf(Operation *op) {
    auto fn = op->getParentOfType<LLVM::LLVMFuncOp>();
    auto it = frames.find(&fn.getBody());
    if (it == frames.end()) {
      op->emitOpError("needs a go.defer_frame in the function");
      return failure();
    }
    return it->second;
  }

  LogicalResult lower(DeferOp op) {
    Location loc = op.getLoc();
    auto fr = frameOf(op);
    auto push = runtimeFunc(op, "deferpush", Type(), {ptrTy});
    if (failed(fr) || failed(push))
      return failure();
    auto fn = op->getParentOfType<LLVM::LLVMFuncOp>();
    LLVM::LLVMStructType recTy = deferRecordType();
    Value rec;
    if (inCycle(fn, op->getBlock())) {
      // One record per iteration, kept until the function returns.
      auto alloc = runtimeFunc(op, "mallocConservative", ptrTy, {i64()});
      if (failed(alloc))
        return failure();
      uint64_t size = DataLayout::closest(op).getTypeSize(recTy);
      Value n = LLVM::ConstantOp::create(b, loc, i64(), size);
      rec = call(loc, *alloc, {n});
    } else {
      rec = entryAlloca(fn, recTy);
    }
    auto field = [&](int32_t i) {
      return LLVM::GEPOp::create(b, loc, ptrTy, recTy, rec, ArrayRef<LLVM::GEPArg>{0, i});
    };
    LLVM::StoreOp::create(b, loc, op.getFn(), field(1));
    LLVM::StoreOp::create(b, loc, *fr, field(2));
    Value wrapped = LLVM::ConstantOp::create(b, loc, b.getI8Type(), op.getWrapped() ? 1 : 0);
    LLVM::StoreOp::create(b, loc, wrapped, field(3));
    call(loc, *push, {rec});
    op.erase();
    return success();
  }

  LogicalResult lower(RunDefersOp op) {
    auto fr = frameOf(op);
    auto fn = runtimeFunc(op, "deferreturn", Type(), {ptrTy});
    if (failed(fr) || failed(fn))
      return failure();
    call(op.getLoc(), *fn, {*fr});
    op.erase();
    return success();
  }

  LogicalResult lower(GoOp op) {
    auto fn = runtimeFunc(op, "newproc", Type(), {ptrTy});
    if (failed(fn))
      return failure();
    call(op.getLoc(), *fn, {op.getFn()});
    op.erase();
    return success();
  }

  LogicalResult lower(PanicOp op) {
    Type ity = ifaceType(b.getContext());
    bool existed = symbols.lookup("runtime.gopanic") != nullptr;
    auto fn = runtimeFunc(op, "gopanic", Type(), {ity});
    if (failed(fn))
      return failure();
    if (!existed)
      addPassthrough(*fn, "noreturn");
    call(op.getLoc(), *fn, {op.getValue()});
    LLVM::UnreachableOp::create(b, op.getLoc());
    op.erase();
    return success();
  }

  LogicalResult lower(RecoverOp op) {
    Location loc = op.getLoc();
    auto fn = runtimeFunc(op, "gorecover", ifaceType(b.getContext()), {i64()});
    if (failed(fn))
      return failure();
    Value zero = LLVM::ConstantOp::create(b, loc, i32(), 0);
    Value fp = LLVM::CallIntrinsicOp::create(b, loc, ptrTy,
                                             b.getStringAttr("llvm.frameaddress.p0"),
                                             ValueRange{zero})
                   .getResult(0);
    Value fpInt = LLVM::PtrToIntOp::create(b, loc, i64(), fp);
    Value r = call(loc, *fn, {fpInt});
    // gorecover checks the caller's frame, so this frame must stay a frame.
    op->getParentOfType<LLVM::LLVMFuncOp>().setNoInline(true);
    op.replaceAllUsesWith(r);
    op.erase();
    return success();
  }

  //===--------------------------------------------------------------------===//
  // Strings family
  //===--------------------------------------------------------------------===//

  LogicalResult lower(StrConcatOp op) {
    Location loc = op.getLoc();
    Type strTy = stringType(b.getContext());
    auto parts = op.getParts();
    Value r;
    if (parts.size() <= 3) {
      SmallVector<Type> params(parts.size(), strTy);
      auto fn = runtimeFunc(op, parts.size() == 2 ? "concatstring2" : "concatstring3",
                            strTy, params);
      if (failed(fn))
        return failure();
      r = call(loc, *fn, parts);
    } else {
      // concatstrings([]string): the operands go to a stack array.
      auto fn = runtimeFunc(op, "concatstrings", strTy, {sliceType(b.getContext())});
      if (failed(fn))
        return failure();
      auto arrTy = LLVM::LLVMArrayType::get(strTy, parts.size());
      Value arr = entryAlloca(op->getParentOfType<LLVM::LLVMFuncOp>(), arrTy);
      LLVM::LifetimeStartOp::create(b, loc, arr);
      for (auto [i, v] : llvm::enumerate(parts)) {
        Value slot = LLVM::GEPOp::create(
            b, loc, ptrTy, arrTy, arr,
            ArrayRef<LLVM::GEPArg>{0, static_cast<int32_t>(i)});
        LLVM::StoreOp::create(b, loc, v, slot);
      }
      Value n = LLVM::ConstantOp::create(b, loc, i64(), parts.size());
      Value sl = LLVM::PoisonOp::create(b, loc, sliceType(b.getContext()));
      sl = LLVM::InsertValueOp::create(b, loc, sl, arr, ArrayRef<int64_t>{0});
      sl = LLVM::InsertValueOp::create(b, loc, sl, n, ArrayRef<int64_t>{1});
      sl = LLVM::InsertValueOp::create(b, loc, sl, n, ArrayRef<int64_t>{2});
      r = call(loc, *fn, {sl});
      // The array is live only around the call, so the slot can be reused.
      LLVM::LifetimeEndOp::create(b, loc, arr);
    }
    op.replaceAllUsesWith(r);
    op.erase();
    return success();
  }

  LogicalResult lower(StrToBytesOp op) {
    auto fn = runtimeFunc(op, "stringtoslicebyte", sliceType(b.getContext()),
                          {stringType(b.getContext())});
    if (failed(fn))
      return failure();
    op.replaceAllUsesWith(call(op.getLoc(), *fn, {op.getStr()}));
    op.erase();
    return success();
  }

  LogicalResult lower(BytesToStrOp op) {
    Location loc = op.getLoc();
    Type strTy = stringType(b.getContext());
    Value r;
    if (op.getNocopy()) {
      // The string aliases the bytes: just drop the capacity.
      Value p = LLVM::ExtractValueOp::create(b, loc, op.getBytes(), ArrayRef<int64_t>{0});
      Value n = LLVM::ExtractValueOp::create(b, loc, op.getBytes(), ArrayRef<int64_t>{1});
      r = LLVM::PoisonOp::create(b, loc, strTy);
      r = LLVM::InsertValueOp::create(b, loc, r, p, ArrayRef<int64_t>{0});
      r = LLVM::InsertValueOp::create(b, loc, r, n, ArrayRef<int64_t>{1});
    } else {
      auto fn = runtimeFunc(op, "slicebytetostring", strTy, {sliceType(b.getContext())});
      if (failed(fn))
        return failure();
      r = call(loc, *fn, {op.getBytes()});
    }
    op.replaceAllUsesWith(r);
    op.erase();
    return success();
  }

  //===--------------------------------------------------------------------===//
  // Maps and channels family
  //===--------------------------------------------------------------------===//

  // A temporary slot of memory type t: an entry block alloca, live from here
  // to endTemp.
  Value temp(Operation *at, Type t) {
    Value p = entryAlloca(at->getParentOfType<LLVM::LLVMFuncOp>(), memoryType(t));
    LLVM::LifetimeStartOp::create(b, at->getLoc(), p);
    return p;
  }
  void endTemp(Location loc, Value p) { LLVM::LifetimeEndOp::create(b, loc, p); }

  // Loads a value of SSA type t from memory (i1 is stored as i8).
  Value loadMem(Location loc, Type t, Value addr) {
    Type mt = memoryType(t);
    Value v = LLVM::LoadOp::create(b, loc, mt, addr);
    if (mt != t)
      v = LLVM::TruncOp::create(b, loc, t, v);
    return v;
  }

  // A temporary holding v; returns its address.
  Value spill(Operation *at, Value v) {
    Value p = temp(at, v.getType());
    storeMem(at->getLoc(), v, p);
    return p;
  }

  LogicalResult lower(MakeMapOp op) {
    auto t = addressOf(op, op.getDescAttr());
    auto fn = runtimeFunc(op, "makemap", ptrTy, {ptrTy, i64()});
    if (failed(t) || failed(fn))
      return failure();
    op.replaceAllUsesWith(call(op.getLoc(), *fn, {*t, op.getHint()}));
    op.erase();
    return success();
  }

  LogicalResult lower(MapLookupOp op) {
    Location loc = op.getLoc();
    auto t = addressOf(op, op.getDescAttr());
    if (failed(t))
      return failure();
    Value k = spill(op, op.getKey());
    Value slot, ok;
    if (op.getCommaok()) {
      auto resTy = LLVM::LLVMStructType::getLiteral(b.getContext(), {ptrTy, b.getI1Type()});
      auto fn = runtimeFunc(op, "mapaccess2", resTy, {ptrTy, ptrTy, ptrTy});
      if (failed(fn))
        return failure();
      Value r = call(loc, *fn, {*t, op.getMap(), k});
      slot = LLVM::ExtractValueOp::create(b, loc, r, ArrayRef<int64_t>{0});
      ok = LLVM::ExtractValueOp::create(b, loc, r, ArrayRef<int64_t>{1});
    } else {
      auto fn = runtimeFunc(op, "mapaccess1", ptrTy, {ptrTy, ptrTy, ptrTy});
      if (failed(fn))
        return failure();
      slot = call(loc, *fn, {*t, op.getMap(), k});
    }
    endTemp(loc, k);
    // The slot is never nil: it points at a zero value when the key is absent.
    op.getResult().replaceAllUsesWith(loadMem(loc, op.getValueType(), slot));
    if (ok)
      op.getOk().replaceAllUsesWith(ok);
    op.erase();
    return success();
  }

  LogicalResult lower(MapUpdateOp op) {
    Location loc = op.getLoc();
    auto t = addressOf(op, op.getDescAttr());
    auto fn = runtimeFunc(op, "mapassign", ptrTy, {ptrTy, ptrTy, ptrTy});
    if (failed(t) || failed(fn))
      return failure();
    Value k = spill(op, op.getKey());
    Value slot = call(loc, *fn, {*t, op.getMap(), k});
    endTemp(loc, k);
    storeMem(loc, op.getValue(), slot);
    op.erase();
    return success();
  }

  LogicalResult lower(MapDeleteOp op) {
    Location loc = op.getLoc();
    auto t = addressOf(op, op.getDescAttr());
    auto fn = runtimeFunc(op, "mapdelete", Type(), {ptrTy, ptrTy, ptrTy});
    if (failed(t) || failed(fn))
      return failure();
    Value k = spill(op, op.getKey());
    call(loc, *fn, {*t, op.getMap(), k});
    endTemp(loc, k);
    op.erase();
    return success();
  }

  LogicalResult lower(MakeChanOp op) {
    auto t = addressOf(op, op.getDescAttr());
    auto fn = runtimeFunc(op, "makechan", ptrTy, {ptrTy, i64()});
    if (failed(t) || failed(fn))
      return failure();
    op.replaceAllUsesWith(call(op.getLoc(), *fn, {*t, op.getSize()}));
    op.erase();
    return success();
  }

  LogicalResult lower(ChanSendOp op) {
    Location loc = op.getLoc();
    auto fn = runtimeFunc(op, "chansend1", Type(), {ptrTy, ptrTy});
    if (failed(fn))
      return failure();
    Value v = spill(op, op.getValue());
    call(loc, *fn, {op.getChan(), v});
    endTemp(loc, v);
    op.erase();
    return success();
  }

  LogicalResult lower(ChanRecvOp op) {
    Location loc = op.getLoc();
    Value slot = temp(op, op.getValueType());
    Value ok;
    if (op.getCommaok()) {
      auto fn = runtimeFunc(op, "chanrecv2", b.getI1Type(), {ptrTy, ptrTy});
      if (failed(fn))
        return failure();
      ok = call(loc, *fn, {op.getChan(), slot});
    } else {
      auto fn = runtimeFunc(op, "chanrecv1", Type(), {ptrTy, ptrTy});
      if (failed(fn))
        return failure();
      call(loc, *fn, {op.getChan(), slot});
    }
    Value v = loadMem(loc, op.getValueType(), slot);
    endTemp(loc, slot);
    op.getResult().replaceAllUsesWith(v);
    if (ok)
      op.getOk().replaceAllUsesWith(ok);
    op.erase();
    return success();
  }

  llvm::DenseMap<Region *, Value> frames;
  llvm::DenseMap<Region *, llvm::DenseSet<Block *>> cyclic;

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

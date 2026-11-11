// Devirt.cpp: devirtualization on the go dialect. Rules in docs/passes.md
// (section go-devirt).
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/Builders.h"
#include "mlir/IR/SymbolTable.h"

namespace marmot::go {
#define GEN_PASS_DEF_GODEVIRT
#include "marmot/Dialect/Go/Passes.h.inc"
} // namespace marmot::go

using namespace mlir;
using namespace marmot::go;

namespace {

// The value at path inside the aggregate built by v (insertvalue chains
// over poison, undef or zero), or null if unknown.
Value fieldOf(Value v, ArrayRef<int64_t> path) {
  while (v) {
    auto ins = v.getDefiningOp<LLVM::InsertValueOp>();
    if (!ins)
      return Value();
    ArrayRef<int64_t> pos = ins.getPosition();
    size_t common = std::min(pos.size(), path.size());
    if (pos.take_front(common) == path.take_front(common)) {
      if (pos.size() == path.size())
        return ins.getValue();
      if (pos.size() < path.size())
        return fieldOf(ins.getValue(), path.drop_front(pos.size()));
      return Value(); // a part of the wanted field was overwritten
    }
    v = ins.getContainer();
  }
  return Value();
}

// G1: the constant initializer of global @sym, if it is a constant with an
// initializer region that the linker may not replace by a different one.
Value initializerOf(SymbolTable &symbols, FlatSymbolRefAttr sym) {
  auto g = symbols.lookup<LLVM::GlobalOp>(sym.getValue());
  if (!g || !g.getConstant() || g.getInitializerRegion().empty())
    return Value();
  switch (g.getLinkage()) {
  case LLVM::Linkage::Private:
  case LLVM::Linkage::Internal:
  case LLVM::Linkage::External:
  case LLVM::Linkage::LinkonceODR:
  case LLVM::Linkage::WeakODR:
    break;
  default:
    return Value();
  }
  auto ret = dyn_cast<LLVM::ReturnOp>(g.getInitializerBlock()->getTerminator());
  return ret && ret.getNumOperands() == 1 ? ret.getOperand(0) : Value();
}

std::optional<StringRef> addressSymbol(Value v) {
  if (auto a = v ? v.getDefiningOp<LLVM::AddressOfOp>() : nullptr)
    return a.getGlobalName();
  return std::nullopt;
}

struct GoDevirtPass : marmot::go::impl::GoDevirtBase<GoDevirtPass> {
  void runOnOperation() override {
    ModuleOp m = getOperation();
    SymbolTable symbols(m);
    OpBuilder b(m.getContext());
    SmallVector<IfaceCallOp> calls;
    SmallVector<TypeAssertOp> asserts;
    m.walk([&](Operation *op) {
      if (auto c = dyn_cast<IfaceCallOp>(op))
        calls.push_back(c);
      else if (auto a = dyn_cast<TypeAssertOp>(op))
        asserts.push_back(a);
    });

    // D1
    for (IfaceCallOp call : calls) {
      auto mk = call.getIface().getDefiningOp<MakeIfaceOp>();
      if (!mk)
        continue;
      Value init = initializerOf(symbols, mk.getTabAttr());
      auto target = addressSymbol(fieldOf(init, {4, static_cast<int64_t>(call.getMethod())}));
      if (!target)
        continue;
      auto fn = symbols.lookup<LLVM::LLVMFuncOp>(*target);
      if (!fn)
        continue;
      SmallVector<Value> args{mk.getData()};
      llvm::append_range(args, call.getArgs());
      LLVM::LLVMFunctionType ft = fn.getFunctionType();
      if (ft.isVarArg() || ft.getNumParams() != args.size() ||
          !llvm::equal(ft.getParams(), ValueRange(args).getTypes()))
        continue;
      Type want = call.getNumResults() ? call.getResult(0).getType()
                                       : LLVM::LLVMVoidType::get(m.getContext());
      if (ft.getReturnType() != want)
        continue;
      b.setInsertionPoint(call);
      auto direct = LLVM::CallOp::create(b, call.getLoc(), fn, args);
      call.replaceAllUsesWith(direct.getResults());
      call.erase();
    }

    // D2
    for (TypeAssertOp ta : asserts) {
      if (ta.getToIface() || ta.getToAny())
        continue;
      auto mk = ta.getX().getDefiningOp<MakeIfaceOp>();
      if (!mk)
        continue;
      std::optional<StringRef> have;
      if (ta.getFromIface())
        have = addressSymbol(fieldOf(initializerOf(symbols, mk.getTabAttr()), {1}));
      else
        have = mk.getTab();
      if (!have || *have != ta.getDesc())
        continue;
      ta.getResult().replaceAllUsesWith(mk.getData());
      if (ta.getOk()) {
        b.setInsertionPoint(ta);
        Value t = LLVM::ConstantOp::create(b, ta.getLoc(), b.getI1Type(), 1);
        ta.getOk().replaceAllUsesWith(t);
      }
      ta.erase();
    }
  }
};

} // namespace

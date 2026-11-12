// BoolPack.cpp: a []bool made by make and used only through indexed loads
// and stores in its function becomes a bit array (8x less memory). Rules
// P1-P3 in analyze.
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/Builders.h"

namespace marmot::go {
#define GEN_PASS_DEF_GOBOOLPACK
#include "marmot/Dialect/Go/Passes.h.inc"
} // namespace marmot::go

using namespace mlir;
using namespace marmot::go;

namespace {

struct Plan {
  MakeSliceOp make;
  SmallVector<LLVM::ExtractValueOp> data, lens; // [0] and [1]/[2] reads
  SmallVector<IndexOp> indexes;
};

// P1: a heap make([]bool). P2: every use of the slice extracts its data
// pointer or a length. P3: every use of the data pointer is a go.index of i8
// whose address is only loaded or stored (i8, not volatile).
std::optional<Plan> analyze(MakeSliceOp mk) {
  if (mk.getStack() || mk.getDesc() != "type:bool" || !mk.getElemType().isInteger(8))
    return std::nullopt;
  Plan p{mk, {}, {}, {}};
  for (Operation *u : mk->getUsers()) {
    auto ev = dyn_cast<LLVM::ExtractValueOp>(u);
    if (!ev || ev.getPosition().size() != 1)
      return std::nullopt;
    if (ev.getPosition()[0] == 0)
      p.data.push_back(ev);
    else
      p.lens.push_back(ev);
  }
  for (LLVM::ExtractValueOp d : p.data)
    for (OpOperand &use : d->getUses()) {
      auto ix = dyn_cast<IndexOp>(use.getOwner());
      if (!ix || ix.getBase() != d.getResult() || !ix.getElemType().isInteger(8))
        return std::nullopt;
      for (OpOperand &au : ix->getUses()) {
        Operation *a = au.getOwner();
        if (auto ld = dyn_cast<LLVM::LoadOp>(a)) {
          if (!ld.getType().isInteger(8) || ld.getVolatile_())
            return std::nullopt;
        } else if (auto st = dyn_cast<LLVM::StoreOp>(a)) {
          if (st.getAddr() != ix.getResult() || !st.getValue().getType().isInteger(8) ||
              st.getVolatile_())
            return std::nullopt;
        } else {
          return std::nullopt;
        }
      }
      p.indexes.push_back(ix);
    }
  return p;
}

void rewrite(Plan &p) {
  MakeSliceOp mk = p.make;
  OpBuilder b(mk);
  Location loc = mk.getLoc();
  Type i64 = mk.getLen().getType();
  Type i8 = b.getI8Type();
  Type ptr = LLVM::LLVMPointerType::get(b.getContext());
  auto c = [&](int64_t v) { return LLVM::ConstantOp::create(b, loc, i64, v).getResult(); };
  // Bytes needed; a negative or huge length goes to the allocator unchanged,
  // so it panics exactly as before.
  auto bytes = [&](Value n) -> Value {
    Value packed = LLVM::LShrOp::create(b, loc, LLVM::AddOp::create(b, loc, n, c(7)), c(3));
    Value huge = LLVM::ICmpOp::create(b, loc, LLVM::ICmpPredicate::ugt, n, c(int64_t(1) << 44));
    return LLVM::SelectOp::create(b, loc, huge, n, packed);
  };
  Value len = mk.getLen(), cap = mk.getCap();
  auto packed = MakeSliceOp::create(b, loc, mk.getDescAttr(), TypeAttr::get(i8),
                                    bytes(len), bytes(cap));
  Value data = LLVM::ExtractValueOp::create(b, loc, packed, ArrayRef<int64_t>{0});
  for (LLVM::ExtractValueOp ev : p.lens) {
    ev.replaceAllUsesWith(ev.getPosition()[0] == 1 ? len : cap);
    ev.erase();
  }
  for (IndexOp ix : p.indexes) {
    OpBuilder ib(ix);
    Location l = ix.getLoc();
    auto k = [&](int64_t v) { return LLVM::ConstantOp::create(ib, l, i64, v).getResult(); };
    // The same bounds check, against the original length.
    BoundsCheckOp::create(ib, l, ix.getIndex(), ix.getLen(), ix.getUnsignedIndexAttr());
    Value byteAddr = LLVM::GEPOp::create(ib, l, ptr, i8, data,
                                         ValueRange{LLVM::LShrOp::create(ib, l, ix.getIndex(), k(3))});
    Value bit = LLVM::TruncOp::create(ib, l, i8, LLVM::AndOp::create(ib, l, ix.getIndex(), k(7)));
    Value mask = LLVM::ShlOp::create(ib, l, LLVM::ConstantOp::create(ib, l, i8, 1), bit);
    for (Operation *u : llvm::make_early_inc_range(ix->getUsers())) {
      OpBuilder ub(u);
      if (auto ld = dyn_cast<LLVM::LoadOp>(u)) {
        Value byte = LLVM::LoadOp::create(ub, l, i8, byteAddr);
        Value v = LLVM::AndOp::create(ub, l, LLVM::LShrOp::create(ub, l, byte, bit),
                                      LLVM::ConstantOp::create(ub, l, i8, 1));
        ld.replaceAllUsesWith(v);
        ld.erase();
      } else {
        auto st = cast<LLVM::StoreOp>(u);
        Value byte = LLVM::LoadOp::create(ub, l, i8, byteAddr);
        Value clear = LLVM::AndOp::create(
            ub, l, byte, LLVM::XOrOp::create(ub, l, mask, LLVM::ConstantOp::create(ub, l, i8, -1)));
        Value v1 = LLVM::AndOp::create(ub, l, st.getValue(), LLVM::ConstantOp::create(ub, l, i8, 1));
        Value nb = LLVM::OrOp::create(ub, l, clear, LLVM::ShlOp::create(ub, l, v1, bit));
        LLVM::StoreOp::create(ub, l, nb, byteAddr);
        st.erase();
      }
    }
    ix.erase();
  }
  for (LLVM::ExtractValueOp d : p.data) {
    d.replaceAllUsesWith(data);
    d.erase();
  }
  mk.erase();
}

struct GoBoolPackPass : marmot::go::impl::GoBoolPackBase<GoBoolPackPass> {
  void runOnOperation() override {
    SmallVector<MakeSliceOp> cands;
    getOperation().walk([&](MakeSliceOp mk) { cands.push_back(mk); });
    for (MakeSliceOp mk : cands)
      if (auto p = analyze(mk))
        rewrite(*p);
  }
};

} // namespace

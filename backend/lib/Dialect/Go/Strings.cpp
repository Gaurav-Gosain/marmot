// Strings.cpp: string concat folding and no-copy conversions. Rules in
// docs/passes.md (section go-strings).
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/Builders.h"
#include "mlir/IR/Matchers.h"
#include "mlir/IR/SymbolTable.h"
#include "mlir/Interfaces/SideEffectInterfaces.h"
#include "llvm/ADT/DenseSet.h"
#include "llvm/Support/FormatVariadic.h"
#include "llvm/Support/xxhash.h"

namespace marmot::go {
#define GEN_PASS_DEF_GOSTRINGS
#include "marmot/Dialect/Go/Passes.h.inc"
} // namespace marmot::go

using namespace mlir;
using namespace marmot::go;

namespace {

// C1: the bytes of a constant string value: llvm.mlir.zero (""), or
// insertvalue %len [1] of insertvalue (addressof @g) [0] of undef, poison or
// zero, where @g is a constant global with a string value at least len long.
std::optional<std::string> constString(Value v, SymbolTable &symbols) {
  if (v.getDefiningOp<LLVM::ZeroOp>())
    return std::string();
  auto lenIns = v.getDefiningOp<LLVM::InsertValueOp>();
  if (!lenIns || lenIns.getPosition() != ArrayRef<int64_t>{1})
    return std::nullopt;
  auto ptrIns = lenIns.getContainer().getDefiningOp<LLVM::InsertValueOp>();
  if (!ptrIns || ptrIns.getPosition() != ArrayRef<int64_t>{0})
    return std::nullopt;
  Operation *base = ptrIns.getContainer().getDefiningOp();
  if (!base || !isa<LLVM::UndefOp, LLVM::PoisonOp, LLVM::ZeroOp>(base))
    return std::nullopt;
  APInt len;
  if (!matchPattern(lenIns.getValue(), m_ConstantInt(&len)))
    return std::nullopt;
  auto addr = ptrIns.getValue().getDefiningOp<LLVM::AddressOfOp>();
  if (!addr)
    return std::nullopt;
  auto g = symbols.lookup<LLVM::GlobalOp>(addr.getGlobalName());
  if (!g || !g.getConstant())
    return std::nullopt;
  auto str = dyn_cast_or_null<StringAttr>(g.getValueOrNull());
  if (!str || len.isNegative() || len.getZExtValue() > str.getValue().size())
    return std::nullopt;
  return str.getValue().take_front(len.getZExtValue()).str();
}

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
  return true;
}

// S3 consumers: they read the string and do not keep it.
bool isTransientUse(OpOperand &use) {
  Operation *u = use.getOwner();
  if (auto ml = dyn_cast<MapLookupOp>(u))
    return use.get() == ml.getKey();
  if (auto md = dyn_cast<MapDeleteOp>(u))
    return use.get() == md.getKey();
  if (isa<StrConcatOp>(u))
    return true;
  if (auto ev = dyn_cast<LLVM::ExtractValueOp>(u))
    return ev.getPosition() == ArrayRef<int64_t>{1};
  if (auto call = dyn_cast<LLVM::CallOp>(u))
    return call.getCallee() &&
           (*call.getCallee() == "runtime.cmpstring" || *call.getCallee() == "runtime.strequal");
  return false;
}

struct GoStringsPass : marmot::go::impl::GoStringsBase<GoStringsPass> {
  void runOnOperation() override {
    ModuleOp m = getOperation();
    SymbolTable symbols(m);
    OpBuilder b(m.getContext());
    Type strTy = stringType(m.getContext());
    Type ptrTy = LLVM::LLVMPointerType::get(m.getContext());

    auto makeConst = [&](Location loc, const std::string &bytes) -> Value {
      if (bytes.empty())
        return LLVM::ZeroOp::create(b, loc, strTy);
      std::string name =
          llvm::formatv(".str.marmot.{0:x-}", llvm::xxh3_64bits(bytes)).str();
      if (!symbols.lookup(name)) {
        OpBuilder::InsertionGuard g(b);
        b.setInsertionPointToStart(m.getBody());
        auto arrTy = LLVM::LLVMArrayType::get(b.getI8Type(), bytes.size());
        auto gl = LLVM::GlobalOp::create(b, loc, arrTy, /*isConstant=*/true,
                                         LLVM::Linkage::Private, name,
                                         b.getStringAttr(bytes));
        gl.setUnnamedAddr(LLVM::UnnamedAddr::Global);
        symbols.insert(gl);
      }
      Value p = LLVM::AddressOfOp::create(b, loc, ptrTy, name);
      Value n = LLVM::ConstantOp::create(b, loc, b.getI64Type(), bytes.size());
      Value s = LLVM::PoisonOp::create(b, loc, strTy);
      s = LLVM::InsertValueOp::create(b, loc, s, p, ArrayRef<int64_t>{0});
      return LLVM::InsertValueOp::create(b, loc, s, n, ArrayRef<int64_t>{1});
    };

    SmallVector<StrConcatOp> concats;
    SmallVector<BytesToStrOp> convs;
    m.walk([&](Operation *op) {
      if (auto c = dyn_cast<StrConcatOp>(op))
        concats.push_back(c);
      else if (auto c = dyn_cast<BytesToStrOp>(op))
        convs.push_back(c);
    });

    // S3 first, so a converted operand is seen by S1/S2 unchanged.
    for (BytesToStrOp conv : convs) {
      if (conv.getNocopy() || conv->use_empty())
        continue;
      Operation *last = nullptr;
      bool ok = true;
      for (OpOperand &use : conv->getUses()) {
        Operation *u = use.getOwner();
        if (u->getBlock() != conv->getBlock() || !isTransientUse(use)) {
          ok = false;
          break;
        }
        if (!last || last->isBeforeInBlock(u))
          last = u;
      }
      for (Operation *op = conv->getNextNode(); ok && op != last; op = op->getNextNode())
        if (mayWrite(op) && !isa<MapLookupOp, StrConcatOp>(op))
          ok = false;
      if (ok)
        conv.setNocopy(true);
    }

    // Inner concats come first in walk order unless block order differs from
    // dominance order; erased ops are remembered and skipped.
    llvm::DenseSet<Operation *> erased;
    for (StrConcatOp c : concats) {
      if (erased.contains(c))
        continue;
      // S1: flatten single-use inner concats.
      SmallVector<Value> parts;
      SmallVector<Operation *> dead;
      std::function<void(Value)> add = [&](Value v) {
        auto inner = v.getDefiningOp<StrConcatOp>();
        if (inner && inner->hasOneUse()) {
          for (Value p : inner.getParts())
            add(p);
          dead.push_back(inner);
          return;
        }
        parts.push_back(v);
      };
      for (Value p : c.getParts())
        add(p);
      // S2: merge adjacent constants, drop empty ones.
      b.setInsertionPoint(c);
      SmallVector<Value> merged;
      std::optional<std::string> run;
      Value runSingle; // the operand, when the run is one non-empty constant
      unsigned runParts = 0;
      auto flush = [&] {
        if (run && !run->empty())
          merged.push_back(runParts == 1 && runSingle ? runSingle
                                                      : makeConst(c.getLoc(), *run));
        run.reset();
        runSingle = Value();
        runParts = 0;
      };
      bool changed = !dead.empty();
      for (Value p : parts) {
        if (auto k = constString(p, symbols)) {
          if (run || k->empty())
            changed = true;
          if (!k->empty()) {
            runSingle = p;
            ++runParts;
          }
          run = run ? *run + *k : *k;
          continue;
        }
        flush();
        merged.push_back(p);
      }
      flush();
      if (!changed)
        continue;
      Value repl;
      if (merged.empty())
        repl = makeConst(c.getLoc(), "");
      else if (merged.size() == 1)
        repl = merged[0];
      else
        repl = StrConcatOp::create(b, c.getLoc(), strTy, merged).getResult();
      c.replaceAllUsesWith(repl);
      c.erase();
      for (Operation *d : llvm::reverse(dead))
        if (d->use_empty()) {
          erased.insert(d);
          d->erase();
        }
    }
  }
};

} // namespace

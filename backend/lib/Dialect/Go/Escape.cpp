// Escape.cpp: escape analysis on the go dialect. See docs/passes.md.
//
// A value "escapes" when it may be reachable after the current call of the
// function returns, or by another goroutine. The analysis follows the SSA
// uses of an allocation's result (and of every value derived from it: GEPs,
// aggregates that contain it, block arguments it flows into) and gives up at
// the first use it does not understand. It is deliberately simple and
// conservative: every unknown use is an escape.
#include "marmot/Dialect/Go/Passes.h"

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/IR/Matchers.h"
#include "mlir/IR/RegionGraphTraits.h"
#include "mlir/IR/SymbolTable.h"
#include "mlir/Interfaces/ControlFlowInterfaces.h"
#include "mlir/Interfaces/DataLayoutInterfaces.h"
#include "llvm/ADT/DenseSet.h"
#include "llvm/ADT/SCCIterator.h"
#include "llvm/ADT/TypeSwitch.h"

namespace marmot::go {
#define GEN_PASS_DEF_GOESCAPE
#include "marmot/Dialect/Go/Passes.h.inc"
} // namespace marmot::go

using namespace mlir;
using namespace marmot::go;

namespace {

constexpr llvm::StringLiteral kNoEscape = "go.noescape";

// Pointers, and aggregates that contain one (slices, strings, interfaces):
// the parameters that get summaries. For an aggregate, "does not escape"
// means no pointer inside it escapes.
bool holdsPointer(Type t) {
  if (isa<LLVM::LLVMPointerType>(t))
    return true;
  if (auto st = dyn_cast<LLVM::LLVMStructType>(t))
    return llvm::any_of(st.getBody(), holdsPointer);
  if (auto at = dyn_cast<LLVM::LLVMArrayType>(t))
    return holdsPointer(at.getElementType());
  return false;
}

class EscapeAnalysis {
public:
  explicit EscapeAnalysis(ModuleOp m) : symbols(m) {}

  // Whether callee parameter i is known not to escape.
  bool paramNoEscape(LLVM::LLVMFuncOp callee, unsigned i) {
    if (i >= callee.getNumArguments())
      return false;
    // C library functions that only read or write through their pointers.
    StringRef n = callee.getSymName();
    if (n == "memmove" || n == "memcpy" || n == "memset" || n == "memcmp" ||
        n == "bcmp" || n == "strlen")
      return true;
    return callee.getArgAttr(i, kNoEscape) ||
           callee.getArgAttr(i, LLVM::LLVMDialect::getNoCaptureAttrName());
  }

  // Follows the uses of root. inCycle: root is created inside a CFG cycle, so
  // two dynamic instances must never be live at once; flowing into a block
  // argument is then treated as an escape. forSummary: stricter rules so the
  // result is valid as a no-capture promise (pointer comparisons escape).
  bool escapes(Value root, bool inCycle, bool forSummary) {
    SmallVector<Value> work{root};
    llvm::SmallPtrSet<Value, 16> seen{root};
    auto taint = [&](Value v) {
      if (seen.insert(v).second)
        work.push_back(v);
    };
    while (!work.empty()) {
      Value v = work.pop_back_val();
      for (OpOperand &use : v.getUses()) {
        Operation *user = use.getOwner();
        unsigned idx = use.getOperandNumber();
        bool esc = TypeSwitch<Operation *, bool>(user)
            .Case([&](LLVM::LoadOp) { return false; })
            .Case([&](LLVM::StoreOp st) { return st.getValue() == v; })
            .Case([&](LLVM::GEPOp g) {
              taint(g.getResult());
              return false;
            })
            .Case([&](LLVM::ICmpOp c) {
              if (!forSummary)
                return false;
              Value other = c.getLhs() == v ? c.getRhs() : c.getLhs();
              return !other.getDefiningOp<LLVM::ZeroOp>();
            })
            .Case([&](LLVM::SelectOp s) {
              if (s.getCondition() == v)
                return false;
              taint(s.getResult());
              return false;
            })
            .Case([&](LLVM::ExtractValueOp e) {
              taint(e.getResult());
              return false;
            })
            .Case([&](LLVM::InsertValueOp i) {
              taint(i.getResult());
              return false;
            })
            .Case([&](LLVM::MemcpyOp) { return false; })
            .Case([&](LLVM::MemmoveOp) { return false; })
            .Case([&](LLVM::MemsetOp) { return false; })
            .Case([&](LLVM::LifetimeStartOp) { return false; })
            .Case([&](LLVM::LifetimeEndOp) { return false; })
            .Case([&](BranchOpInterface br) {
              if (inCycle)
                return true;
              if (auto arg = br.getSuccessorBlockArgument(idx))
                taint(*arg);
              else
                return true; // not a successor operand (a condition, say)
              return false;
            })
            .Case([&](LLVM::CallOp call) {
              auto callee = call.getCallee();
              if (!callee)
                return true; // indirect call
              auto fn = symbols.lookup<LLVM::LLVMFuncOp>(*callee);
              if (!fn)
                return true;
              return !paramNoEscape(fn, idx);
            })
            // go dialect
            .Case([&](NilCheckOp) { return false; })
            .Case([&](IndexOp o) {
              taint(o.getResult());
              return false;
            })
            .Case([&](SliceOp o) {
              taint(o.getResult());
              return false;
            })
            .Case([&](MakeIfaceOp o) {
              taint(o.getResult());
              return false;
            })
            .Case([&](TypeAssertOp o) {
              taint(o.getResult());
              return false;
            })
            .Case([&](BoxOp o) {
              taint(o.getResult());
              return false;
            })
            .Case([&](MakeClosureOp o) {
              taint(o.getResult());
              return false;
            })
            .Case([&](BytesToStrOp o) {
              if (o.getNocopy())
                taint(o.getResult());
              return false;
            })
            .Case([&](StrToBytesOp) { return false; })
            .Case([&](StrConcatOp) { return false; })
            .Case([&](MapLookupOp o) { return false; })
            .Case([&](MapDeleteOp o) { return false; })
            // A deferred call runs before the frame is gone, but a record
            // made in a loop outlives the iteration.
            .Case([&](DeferOp) { return inCycle; })
            .Default([&](Operation *) { return true; });
        if (esc)
          return true;
      }
    }
    return false;
  }

  // Computes go.noescape for every pointer parameter of every defined
  // function: start from "no escape" and remove until nothing changes.
  void summarize(ModuleOp m) {
    SmallVector<LLVM::LLVMFuncOp> fns;
    m.walk([&](LLVM::LLVMFuncOp f) {
      if (!f.isExternal()) {
        fns.push_back(f);
        for (unsigned i = 0; i < f.getNumArguments(); ++i)
          if (holdsPointer(f.getArgument(i).getType()))
            f.setArgAttr(i, kNoEscape, UnitAttr::get(m.getContext()));
      }
    });
    bool changed = true;
    while (changed) {
      changed = false;
      for (LLVM::LLVMFuncOp f : fns)
        for (unsigned i = 0; i < f.getNumArguments(); ++i)
          if (f.getArgAttr(i, kNoEscape) &&
              escapes(f.getArgument(i), /*inCycle=*/false, /*forSummary=*/true)) {
            f.removeArgAttr(i, kNoEscape);
            changed = true;
          }
    }
  }

private:
  SymbolTable symbols;
};

} // namespace

void marmot::go::summarizeEscapes(ModuleOp m) {
  EscapeAnalysis ea(m);
  ea.summarize(m);
}

namespace {

struct GoEscapePass : marmot::go::impl::GoEscapeBase<GoEscapePass> {
  using GoEscapeBase::GoEscapeBase;

  void runOnOperation() override {
    ModuleOp m = getOperation();
    EscapeAnalysis ea(m);
    ea.summarize(m);
    m.walk([&](LLVM::LLVMFuncOp fn) {
      if (fn.isExternal())
        return;
      DataLayout dl = DataLayout::closest(fn);
      llvm::DenseSet<Block *> cyclic;
      for (auto scc = llvm::scc_begin(&fn.getBody()); !scc.isAtEnd(); ++scc)
        if (scc->size() > 1 || scc.hasCycle())
          for (Block *b : *scc)
            cyclic.insert(b);
      uint64_t frame = 0;
      fn.walk([&](Operation *op) {
        std::optional<uint64_t> size = stackSize(op, dl);
        if (!size || *size > maxObjectSize || frame + *size > maxFrameSize)
          return;
        if (ea.escapes(op->getResult(0), cyclic.contains(op->getBlock()), false))
          return;
        op->setAttr("stack", UnitAttr::get(op->getContext()));
        frame += *size;
      });
    });
  }

  // Size in bytes of the stack slot op would need, or nothing if op cannot
  // move to the stack.
  static std::optional<uint64_t> stackSize(Operation *op, DataLayout &dl) {
    return TypeSwitch<Operation *, std::optional<uint64_t>>(op)
        .Case([&](NewOp o) -> std::optional<uint64_t> {
          if (o.getStack())
            return std::nullopt;
          return dl.getTypeSize(o.getElemType());
        })
        .Case([&](BoxOp o) -> std::optional<uint64_t> {
          if (o.getStack())
            return std::nullopt;
          return dl.getTypeSize(memoryType(o.getValue().getType()));
        })
        .Case([&](MakeClosureOp o) -> std::optional<uint64_t> {
          if (o.getStack())
            return std::nullopt;
          return dl.getTypeSize(o.getEnvType());
        })
        .Case([&](MakeSliceOp o) -> std::optional<uint64_t> {
          APInt len, cap;
          if (o.getStack() || !matchPattern(o.getLen(), m_ConstantInt(&len)) ||
              !matchPattern(o.getCap(), m_ConstantInt(&cap)) || len.isNegative() ||
              cap.slt(len) || cap.getActiveBits() > 32)
            return std::nullopt;
          return cap.getZExtValue() * dl.getTypeSize(o.getElemType());
        })
        .Default([](Operation *) { return std::nullopt; });
  }
};

} // namespace

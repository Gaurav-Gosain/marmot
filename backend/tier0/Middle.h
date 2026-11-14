// Middle.h: the target independent part of compiling one function. FuncPlan
// decides, before any instruction is written, which operations are emitted
// at their users (fused compares, folded GEPs, lazy loads and aggregates),
// which nil checks are already decided and which blocks are dead, which
// values live in registers (linear scan over a target's register sets), the
// frame layout with stack slots shared by values that are not live together,
// and the stack maps of the calls. The target emitters (EmitArm64.cpp,
// EmitX64.cpp) derive from it and only turn these decisions into
// instructions.
#ifndef MARMOT_TIER0_MIDDLE_H
#define MARMOT_TIER0_MIDDLE_H

#include "Module.h"

#include "llvm/ADT/ArrayRef.h"
#include "llvm/ADT/DenseMap.h"
#include "llvm/ADT/DenseSet.h"

#include <map>
#include <vector>

namespace marmot {
namespace tier0 {

// Values to registers (from regs, all below 32); values of the operations in
// skip are never materialized. Values that no call, memory intrinsic or
// return meets after their definition may take the low registers first.
llvm::DenseMap<Value, unsigned> linearScan(LL::LLVMFuncOp fn, llvm::ArrayRef<unsigned> regs,
                                           const llvm::DenseSet<Operation *> &skip,
                                           const llvm::DenseSet<StringAttr> &twice,
                                           llvm::ArrayRef<unsigned> low = {});
bool callsReturnsTwice(LL::LLVMFuncOp fn, const llvm::DenseSet<StringAttr> &twice);

class FuncPlan {
public:
  FuncPlan(ModuleGen &mg, LL::LLVMFuncOp f) : mg(mg), fn(f) {}
  virtual ~FuncPlan() = default;

protected:
  ModuleGen &mg;
  LL::LLVMFuncOp fn;

  // ---- target hooks ----
  // The outgoing stack argument bytes of call, and whether a result of
  // type t is returned in memory.
  virtual uint64_t outgoingArgBytes(LL::CallOp call) = 0;
  virtual bool resultInMemory(Type t) = 0;

  // ---- decisions ----
  llvm::DenseMap<Value, uint64_t> slot;        // value -> frame offset from sp
  llvm::DenseMap<Value, unsigned> home;        // value -> register
  llvm::DenseSet<Operation *> fusedCmp;        // emitted at their use: icmps (at the cond_br), lazy loads
  llvm::DenseSet<Operation *> lazyLoad;        // aggregate loads read field by field from memory at each use
  llvm::DenseMap<Operation *, uint64_t> foldGep;  // constant offset GEPs folded into their loads and stores
  // Nil checks of a pointer already checked on the only path in: the
  // cond_br decided (its taken block), and blocks no live edge reaches.
  llvm::DenseMap<Operation *, Block *> decidedBr;
  llvm::DenseSet<Block *> deadBlock;
  // Aggregates built by an insertvalue chain of 8-byte scalars that only a
  // call uses (a string literal {ptr, len}): never made in a slot, the call
  // puts each field in its argument register (absent fields are zero).
  llvm::DenseMap<Value, std::map<uint64_t, Value>> lazyAgg;
  llvm::DenseSet<Operation *> lazyChainOps;
  // Stack maps: the map of each call (computeStackMaps), the function's
  // distinct maps, and the sites emitted (return pc offset from fnStart).
  llvm::DenseMap<Operation *, uint32_t> callMap;
  std::vector<std::pair<uint32_t, std::string>> smaps;
  std::vector<std::pair<uint64_t, uint32_t>> smSites;
  uint64_t fnStart = 0;
  uint32_t calleeUsed = 0;                     // bit r: register r is a home, saved in the prologue
  uint64_t saveArea = 0;
  llvm::DenseMap<Operation *, uint64_t> allocaStorage;
  uint64_t frameSize = 0, tempArea = 0, tempSize = 0;
  uint64_t sretSlot = 0;  // where the incoming result pointer is kept, when the result is returned in memory
  bool sret = false;

  // Fused compares, decided nil checks, lazy aggregate arguments, folded
  // GEPs and lazy loads.
  void analyze();
  // The frame, from the bottom: outgoing arguments, the callee saved
  // registers in calleeUsed, the result pointer, slots of at most 32 bytes
  // (near sp), larger slots and alloca storage, the block argument
  // temporaries. Run after the register assignment (home).
  void layout();
  void computeStackMaps();

  // Frame layout.
  uint64_t alloc(uint64_t size, uint64_t align) {
    frameSize = (frameSize + align - 1) & ~(align - 1);
    uint64_t at = frameSize;
    frameSize += size;
    return at;
  }
  bool constBits(Value v, uint64_t &bits) { return scalarConst(v, bits); }
  uint64_t of(Value v) {
    auto it = slot.find(v);
    if (it == slot.end()) unsupported("value without a slot");
    return it->second;
  }
  // A zero aggregate used only as the value of stores: never made in its
  // slot, each store writes zeros.
  bool storedZero(Value v) {
    if (!v.getDefiningOp<LL::ZeroOp>() || !isAggregate(v.getType())) return false;
    return llvm::all_of(v.getUses(), [&](OpOperand &u) {
      auto st = dyn_cast<LL::StoreOp>(u.getOwner());
      return st && st.getValue() == v && st.getAddr() != v &&
             st.getOrdering() == LL::AtomicOrdering::not_atomic;
    });
  }

private:
  // Per value of vals: its first and last live position (a hull over the
  // liveness dataflow; block arguments include their predecessors' branches).
  std::vector<std::pair<unsigned, unsigned>> slotHulls(const std::vector<Value> &vals);
  void findLazyAggArgs();
  void findDecidedNilChecks();
};

} // namespace tier0
} // namespace marmot

#endif

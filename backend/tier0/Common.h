// Common.h: what every part of tier0 shares: unsupported constructs, the
// layout of llvm dialect types, symbol names and small facts about values.
#ifndef MARMOT_TIER0_COMMON_H
#define MARMOT_TIER0_COMMON_H

#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/Dialect/LLVMIR/LLVMTypes.h"
#include "mlir/IR/BuiltinTypes.h"
#include "mlir/IR/Location.h"
#include "llvm/ADT/TypeSwitch.h"
#include "llvm/Support/raw_ostream.h"

#include <cstdint>
#include <map>
#include <mutex>
#include <optional>
#include <string>
#include <utility>
#include <vector>

namespace marmot {
namespace tier0 {

using namespace mlir;
namespace LL = mlir::LLVM;

// Unsupported constructs unwind out of the function being compiled; it
// falls back to LLVM.
struct Unsupported {
  std::string why;
};
[[noreturn]] void unsupported(const std::string &why);

// ---- types and layout (the default data layout of arm64 and x86-64) ----

uint64_t sizeOf(Type t);
uint64_t alignOf(Type t);
uint64_t fieldOffset(LL::LLVMStructType s, unsigned idx);
inline bool isAggregate(Type t) { return isa<LL::LLVMStructType, LL::LLVMArrayType>(t); }
inline bool isFloat(Type t) { return t.isF32() || t.isF64(); }
inline unsigned intWidth(Type t) {
  if (auto i = dyn_cast<IntegerType>(t)) return i.getWidth();
  return 64;  // pointers
}
inline bool isWide(Type t) { return isa<IntegerType>(t) && cast<IntegerType>(t).getWidth() == 128; }
// Offset and type of the element at a position path inside an aggregate.
std::pair<uint64_t, Type> elementAt(Type t, ArrayRef<int64_t> pos);
// Scalar pieces of a value in memory order, as LLVM splits aggregates for
// calls and returns: offset and type of each.
void flatten(Type t, uint64_t base, std::vector<std::pair<uint64_t, Type>> &out);
inline unsigned log2Size(uint64_t n) { return n == 1 ? 0 : n == 2 ? 1 : n == 4 ? 2 : 3; }
// A scalar constant has no slot: its canonical bits are made where it is used.
bool scalarConst(Value v, uint64_t &bits);

// Symbol names: Mach-O prefixes C names with an underscore, ELF does not.
extern thread_local bool elfNames;
inline std::string symName(StringRef sym) { return (elfNames ? "" : "_") + sym.str(); }
inline std::string machoName(StringRef sym) { return symName(sym); }

inline bool isLocalLinkage(LL::Linkage l) { return l == LL::Linkage::Internal || l == LL::Linkage::Private; }
template <class Op> bool isHidden(Op op) { return op.getVisibility_() == LL::Visibility::Hidden; }
inline bool isWeakLinkage(LL::Linkage l) {
  return l == LL::Linkage::Linkonce || l == LL::Linkage::LinkonceODR || l == LL::Linkage::Weak ||
         l == LL::Linkage::WeakODR;
}
bool isSignedPred(LL::ICmpPredicate p);

// The source line of a location: the innermost inlined position, else the
// first position a fused location holds.
std::optional<std::pair<StringRef, unsigned>> lineOf(Location l);

// Where one register sized piece of a call argument goes (Abi.h).
struct ArgLoc {
  unsigned arg;   // argument index
  uint64_t off;   // offset of the piece in the argument value
  Type t;
  bool fp;
  int reg;        // register number, or -1 for the stack
  uint64_t stack; // stack offset when reg < 0
};

// TIER0_OPSTATS: code bytes per operation kind, over every emit.
extern std::mutex opBytesMu;
extern std::map<std::string, uint64_t> opBytesAll;
extern const bool opStatsOn;

} // namespace tier0
} // namespace marmot

#endif

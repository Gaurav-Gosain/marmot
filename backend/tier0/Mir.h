// Mir.h: tier0's own function IR (MIR), between the llvm dialect and the
// target emitters.
//
// A function is a list of blocks of instructions over scalar SSA values.
// Aggregates (structs, arrays, i128) do not exist here: the builder splits
// every aggregate value into its scalar pieces (the order and offsets of
// flatten), so insertvalue and extractvalue cost nothing, an aggregate load
// becomes one load per piece that is used, and a call or return passes the
// pieces where the calling convention wants them. Integers are canonical as
// in the rest of tier0: zero extended from their width to 64 bits.
//
// Values are numbered densely; each is defined once, by an instruction or as
// a block parameter. Instructions live in one vector, each block a
// contiguous range of it, so passes are loops over arrays.
#ifndef MARMOT_TIER0_MIR_H
#define MARMOT_TIER0_MIR_H

#include "Module.h"

#include "llvm/ADT/DenseMap.h"

#include <cstdint>
#include <string>
#include <vector>

namespace marmot {
namespace tier0 {
namespace mir {

using VId = uint32_t;
constexpr VId None = ~0u;

// The class of a value: an integer or pointer of width bits (canonical,
// zero extended), or a float (its bits; F32 zero extended from 32).
enum class Cls : uint8_t { Int, F32, F64 };

struct Val {
  Cls cls = Cls::Int;
  uint8_t width = 64;    // integers: 1..64
  bool ptr = false;      // a pointer (for stack maps)
  uint32_t def = ~0u;    // defining instruction, or ~0u for a block parameter
  uint32_t uses = 0;     // number of operand uses (kept by the builder and DCE)
};

enum class Op : uint8_t {
  // Pure value operations.
  Const,     // imm (canonical bits)
  Addr,      // address of symbol sym + imm
  Frame,     // address of frame object aux + imm
  Copy,      // a (only transiently; the builder forwards copies)
  Add, Sub, Mul, UDiv, SDiv, URem, SRem, And, Or, Xor,
  Shl, LShr, AShr,  // shift amount mod 64, on the 64-bit value; then normalized
  UMulH,     // high 64 bits of the unsigned 128-bit product
  ICmp,      // aux: LL::ICmpPredicate; operands of width a's width
  Select,    // a ? b : c
  SExt,      // a, of width aux, sign extended to the result width
  Trunc,     // a, to the result width
  FAdd, FSub, FMul, FDiv, FMin, FMax, FMinNum, FMaxNum, FNeg,
  FMA,       // a * b + c, fused
  FMulAdd,   // a * b + c, fused where the target fuses (llvm.fmuladd)
  FCmp,      // aux: LL::FCmpPredicate
  SIToFP, UIToFP, FPToSI, FPToUI, FPToSISat, FPToUISat, FPExt, FPTrunc,
  Bits,      // a reinterpreted in the result's class (int <-> float)
  Ctlz, Cttz, Ctpop, Bswap, Bitrev,  // on width bits of a (the result width)
  Fshl, Fshr,                         // a, b, c (amount)
  FrameAddr, // the frame pointer
  // Memory and effects.
  Load,      // [a + imm], aux: size in bytes (1, 2, 4, 8); flags: atomic;
             // with an index c: [a + (c << imm2) + imm] (imm2 0..3; not atomic)
  Store,     // [a + imm] = b, aux: size; flags: atomic (seq_cst on x86-64);
             // with an index c as for Load
  Zero,      // aux2 bytes of zeros at [a + imm]
  Copy8,     // memory copy of aux2 bytes from [b + imm2] to [a + imm] (no overlap)
  MemCall,   // memmove / memset: aux 0 memmove(a, b, c), 1 memset(a, b, c)
  Call,      // extra: index of the call record
  AtomicRMW, // aux: LL::AtomicBinOp; [a] op= b, result the old value
  CmpXchg,   // [a]: compare b, swap c; result the old value, dst2 the success bit
  Fence,
  // Terminators.
  Br,        // extra: edge
  CondBr,    // a: condition; extra: true edge, extra + 1: false edge
  Switch,    // a: value; extra: switch record
  Ret,       // extra: return record (the pieces)
  Unreachable,
};

inline bool isTerminator(Op o) { return o >= Op::Br; }
// No effect but its result: removable when unused, and equal operands give
// equal results (CSE).
inline bool isPure(Op o) { return o < Op::Load && o != Op::FrameAddr; }

enum Flags : uint8_t { Atomic = 1, Volatile = 2 };

struct Inst {
  Op op;
  uint8_t w = 64;        // the operation's width: integer operands are read at it; for
                         // conversions and fcmp, the operand's width
  uint8_t flags = 0;
  bool dead = false;     // removed (DCE, fused into its user)
  uint16_t aux = 0;
  VId dst = None;
  VId a = None, b = None, c = None;
  int64_t imm = 0;
  int64_t imm2 = 0;
  uint32_t aux2 = 0;
  uint32_t extra = 0;
  uint32_t sym = 0;           // symbol index (Addr, direct Call)
  mlir::Operation *src = nullptr;  // the llvm operation (locations, calls)
};

// A branch edge: the target and the values it passes to its parameters.
struct Edge {
  uint32_t block;
  uint32_t argBegin, argEnd;  // in Func::edgeArgs
};

// A call: the callee (symbol or a value), the argument pieces in flatten
// order of each argument (the ABI assigns them), the result pieces.
struct CallRec {
  bool indirect = false;
  VId callee = None;
  uint32_t sym = 0;
  mlir::Operation *op = nullptr;  // the LL::CallOp (types, attributes)
  std::vector<VId> args;          // pieces, in order
  std::vector<VId> results;       // pieces, in order (empty for void)
  int sretObj = -1;               // frame object of a result returned in memory
};

struct SwitchRec {
  std::vector<uint64_t> values;
  std::vector<uint32_t> edges;  // per case, then the default last
};

struct RetRec {
  std::vector<VId> pieces;
};

// Storage in the frame that is not a value: allocas and results returned
// in memory.
struct FrameObj {
  uint64_t size, align;
};

struct BB {
  std::vector<VId> params;
  uint32_t begin = 0, end = 0;  // instructions [begin, end), the last a terminator
  std::vector<uint32_t> preds;  // predecessor blocks
  bool dead = false;            // reached by no live edge (decided nil checks)
};

struct Func {
  LL::LLVMFuncOp fn;
  std::vector<Val> vals;
  std::vector<Inst> insts;
  std::vector<BB> blocks;
  std::vector<VId> edgeArgs;
  std::vector<Edge> edges;
  std::vector<CallRec> calls;
  std::vector<SwitchRec> switches;
  std::vector<RetRec> rets;
  std::vector<FrameObj> objs;
  std::vector<std::string> syms;
  llvm::StringMap<uint32_t> symIndex;
  std::vector<VId> params;       // the entry block's pieces, in flatten order per argument
  bool hasSetjmp = false;        // calls a returns_twice function

  uint32_t sym(llvm::StringRef name) {
    auto [it, fresh] = symIndex.try_emplace(name, uint32_t(syms.size()));
    if (fresh) syms.push_back(name.str());
    return it->second;
  }
};

// Builds f's MIR from the llvm dialect function; throws Unsupported for
// anything it does not cover (the function then takes the older path).
// x64 selects the target's rule for results returned in memory.
void build(ModuleGen &mg, LL::LLVMFuncOp fn, Func &f, bool x64);

// Text form, for tests and debugging (TIER0_MIR_DUMP).
std::string print(const Func &f);

} // namespace mir
} // namespace tier0
} // namespace marmot

#endif

// Abi.h: where the C calling convention, as LLVM lowers it, puts the pieces
// of arguments and results, per target.
#ifndef MARMOT_TIER0_ABI_H
#define MARMOT_TIER0_ABI_H

#include "Common.h"

namespace marmot {
namespace tier0 {

// Where LLVM's Darwin arm64 lowering puts each register sized piece of a
// call's arguments: x0-x7 and v0-v7, then the stack, packed at each piece's
// natural size and alignment. A top-level array whose pieces are all i64 /
// ptr, all f32 or all f64 is a block: all of it in registers, or all of it on
// the stack with the remaining registers of its class marked used.
std::vector<ArgLoc> assignArgs(TypeRange types, uint64_t &stackSize);
// True when LLVM returns a value of type t in memory (through the pointer
// in x8): more than 8 general or 8 floating point pieces.
bool returnsInMemory(Type t);

// x86-64 SysV: integers and pointers in rdi, rsi, rdx, rcx, r8, r9, floats
// in xmm0-7, the rest in 8-byte stack slots in order (sret takes rdi).
std::vector<ArgLoc> assignArgsX64(TypeRange types, uint64_t &stackSize, bool sret);
// Results: up to three integer pieces (rax, rdx, rcx) and four float pieces
// (xmm0, xmm1, then the x87 st0, st1), as llc assigns them; more and the
// result goes through a pointer passed in rdi, returned again in rax.
bool returnsInMemoryX64(Type t);
extern const unsigned retGpr[3];

} // namespace tier0
} // namespace marmot

#endif

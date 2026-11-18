// MirAlloc.h: register assignment and frame layout for a MIR function.
//
// Positions number the live instructions in block order (a block's start
// first), except that a block that only panics comes right after its one
// predecessor. A call followed by unreachable clobbers nothing. Every value gets an interval from its definition to its last use,
// over the liveness dataflow when it is used in another block; block
// parameters are written by the edge moves at the end of each predecessor.
// A linear scan then gives each interval one register for its whole length:
// a caller saved one when no call (or library call) lies strictly inside it,
// else a callee saved one, else a stack slot. Constants and frame addresses
// are made again at each use and take no register. Slots are 8 bytes and
// shared by intervals that do not overlap.
#ifndef MARMOT_TIER0_MIRALLOC_H
#define MARMOT_TIER0_MIRALLOC_H

#include "Mir.h"

#include <vector>

namespace marmot {
namespace tier0 {
namespace mir {

struct Loc {
  enum K : uint8_t { None, Reg, Slot, Remat } k = None;
  uint8_t reg = 0;
  uint32_t slot = 0;  // spill slot index
};

// One register class: the registers to use, in order of preference.
struct RegClass {
  std::vector<unsigned> caller;  // not preserved by calls
  std::vector<unsigned> callee;  // preserved by calls (saved in the prologue when used)
};

struct TargetRegs {
  RegClass gpr, fpr;
  bool fmaIsCall = false;  // FMA is a call to the C library (x86-64-v2)
  // Two-address operations (x86-64): the result of an integer add, sub,
  // and, or, xor, mul or shift may take the register of its first operand
  // when that operand's interval ends there (the emitter skips the mov).
  bool tieFirst = false;
};

struct Alloc {
  std::vector<Loc> loc;        // per value
  uint32_t calleeGpr = 0;      // bit r: callee saved general register r used
  uint32_t calleeFpr = 0;
  uint32_t slots = 0;          // number of 8-byte spill slots
  // Per instruction: whether it is emitted at its user instead (a compare
  // fused into its conditional branch).
  std::vector<char> fused;
  // Input: per value, the register it would best live in (where a call,
  // the return or the prologue moves it), or -1. Taken when free.
  std::vector<int16_t> hint;
};

bool isCall(const Inst &in, const TargetRegs &t);
void allocate(const Func &f, const TargetRegs &t, Alloc &a);

} // namespace mir
} // namespace tier0
} // namespace marmot

#endif

// MirEmit.h: what the MIR emitters of every target share: the parallel move
// (edges, call arguments and results, returns, incoming parameters) over
// abstract places, ordered so that no source is overwritten before it is
// read, with cycles broken through one scratch register per class.
#ifndef MARMOT_TIER0_MIREMIT_H
#define MARMOT_TIER0_MIREMIT_H

#include "MirAlloc.h"

#include <vector>

namespace marmot {
namespace tier0 {
namespace mir {

// A place a move reads or writes.
struct Place {
  enum K : uint8_t {
    None,   // nothing (an undefined source, or a result nobody uses)
    Reg,    // register reg of class fp
    Slot,   // spill slot
    Mem,    // [base + off], size bytes (stack arguments, incoming or outgoing)
    Remat,  // the value of instruction inst (a constant or frame address), sources only
  } k = None;
  bool fp = false;
  uint8_t reg = 0;
  uint8_t size = 8;
  uint32_t slot = 0;
  unsigned base = 0;
  int64_t off = 0;
  uint32_t inst = 0;

  static Place reg_(unsigned r, bool fp) {
    Place p;
    p.k = Reg;
    p.reg = uint8_t(r);
    p.fp = fp;
    return p;
  }
  bool same(const Place &o) const {
    if (k != o.k) return false;
    switch (k) {
    case Reg: return reg == o.reg && fp == o.fp;
    case Slot: return slot == o.slot;
    case Mem: return base == o.base && off == o.off;
    default: return false;
    }
  }
  // Whether writing this place may change what o reads.
  bool overlaps(const Place &o) const {
    if (k == Reg && o.k == Reg) return reg == o.reg && fp == o.fp;
    if (k == Slot && o.k == Slot) return slot == o.slot;
    if (k == Mem && o.k == Mem) return base == o.base && off < o.off + o.size && o.off < off + size;
    return false;
  }
};

struct Move {
  Place dst, src;
};

// What a target provides for moves.
class MoveOps {
public:
  virtual ~MoveOps() = default;
  // Copies src to dst; at most one of them is not a register, unless the
  // target goes through scratch (it may use its move scratch registers).
  virtual void move(const Place &dst, const Place &src) = 0;
  // A register of class fp no value lives in and no move names: the cycle
  // breaker.
  virtual unsigned cycleScratch(bool fp) = 0;
};

// Performs the moves as if all sources were read first.
void parallelMove(std::vector<Move> moves, MoveOps &ops);

} // namespace mir
} // namespace tier0
} // namespace marmot

#endif

// MirArm64.cpp: the arm64 emitter for MIR (Darwin calling convention as
// LLVM lowers it). Values live where MirAlloc put them: a register for the
// whole interval, a spill slot, or made again at each use.
//
// Frame, from sp up: outgoing stack arguments, the callee saved registers
// used, the spill slots (8 bytes each), the saved result pointer (x8) of a
// function returning in memory, the frame objects (allocas and results of
// calls returned in memory); then fp, lr.
#include "Abi.h"
#include "Arm64.h"
#include "MirEmit.h"

#include <atomic>
#include <chrono>
#include <cstring>

namespace marmot {
namespace tier0 {

bool mirArm64(ModuleGen &mg, LL::LLVMFuncOp fn);

namespace {

using namespace a64;
using namespace mir;

// Scratch: x9-x11 inside an operation, x16/x17 for addresses and the
// indirect callee, v29-v31 for floats. The move scratch registers (x10,
// v30) and the cycle breakers (x11, v31) are never allocated.
constexpr unsigned S0 = 9, S1 = 10, S2 = 11, SA = 16, SC = 17;
constexpr unsigned F0 = 29, F1 = 30, F2 = 31;

const TargetRegs &regs() {
  static const TargetRegs t = [] {
    TargetRegs r;
    r.gpr.caller = {0, 1, 2, 3, 4, 5, 6, 7, 8, 12, 13, 14, 15};
    r.gpr.callee = {19, 20, 21, 22, 23, 24, 25, 26, 27, 28};
    r.fpr.caller = {0, 1, 2, 3, 4, 5, 6, 7, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28};
    r.fpr.callee = {8, 9, 10, 11, 12, 13, 14, 15};
    return r;
  }();
  return t;
}

class Emitter : public MoveOps {
public:
  Emitter(ModuleGen &mg, LL::LLVMFuncOp fn) : mg(mg), w(mg.w), fn(fn) {}
  void run();

private:
  ModuleGen &mg;
  MachOWriter &w;
  LL::LLVMFuncOp fn;
  Func f;
  Alloc al;

  // Frame.
  uint64_t frameSize = 0, saveArea = 0, slotBase = 0, sretSave = 0;
  std::vector<uint64_t> objOff;
  bool sret = false;
  std::vector<unsigned> savedGpr, savedFpr;

  // Code.
  uint64_t fnStart = 0;
  std::vector<uint64_t> blockStart;
  uint32_t nextBlock = 0;
  struct Fixup {
    uint64_t at;
    uint32_t block;
    enum { B, BCond, Cbnz, Cbz } kind;
    Cond cond;
    unsigned reg;
  };
  std::vector<Fixup> fixups;
  int64_t epiAt = -1;

  void e(uint32_t word) { w.emit32(word); }
  uint64_t here() const { return w.size(Sect::Text); }

  // ---- frame access ----
  uint64_t slotOff(uint32_t s) const { return slotBase + 8 * uint64_t(s); }
  void addConst(unsigned rd, unsigned rn, int64_t off);
  void movImm(unsigned rd, uint64_t v);
  // [base + off] of size bytes, in one instruction when the offset allows
  // (scaled unsigned, or signed 9 bits), else through SA.
  void mem(bool load, bool fp, unsigned rt, unsigned base, int64_t off, unsigned size);
  void ld(unsigned rt, unsigned base, int64_t off, unsigned size) { mem(true, false, rt, base, off, size); }
  void st(unsigned rt, unsigned base, int64_t off, unsigned size) { mem(false, false, rt, base, off, size); }

  // ---- values ----
  bool isFp(VId v) const {
    if (v == None) unsupported("mir: no value");
    return f.vals[v].cls != Cls::Int;
  }
  bool dbl(VId v) const { return f.vals[v].cls == Cls::F64; }
  const Inst &defOf(VId v) const { return f.insts[f.vals[v].def]; }
  bool konst(VId v, uint64_t &bits) const {
    if (al.loc[v].k != Loc::Remat) return false;
    const Inst &d = defOf(v);
    if (d.op != Op::Const) return false;
    bits = uint64_t(d.imm);
    return true;
  }
  void remat(unsigned r, VId v);              // a constant or frame address into GPR r
  // The base register and offset of [v + imm]: a frame object's address
  // folds into sp + offset.
  std::pair<unsigned, int64_t> addrOf(VId v, int64_t imm, unsigned scratch) {
    if (al.loc[v].k == Loc::Remat) {
      const Inst &d = defOf(v);
      if (d.op == Op::Frame) return {SP, int64_t(objOff[d.aux]) + d.imm + imm};
      remat(scratch, v);  // a constant address: never xzr (31 is sp in addresses)
      return {scratch, imm};
    }
    return {gp(v, scratch), imm};
  }
  unsigned gp(VId v, unsigned scratch);       // the GPR holding integer v
  unsigned fr(VId v, unsigned scratch);       // the FPR holding float v
  unsigned gpSigned(VId v, unsigned w, unsigned scratch);  // v sign extended from w bits
  unsigned dstG(VId v, unsigned scratch) {
    if (v != None && al.loc[v].k == Loc::Reg) return al.loc[v].reg;
    return scratch;
  }
  unsigned dstF(VId v, unsigned scratch) { return dstG(v, scratch); }
  void finG(VId v, unsigned r);
  void finF(VId v, unsigned r);
  void norm(unsigned r, unsigned w) {
    if (w < 64) e(ubfx0(r, r, w));
  }
  Place place(VId v) const;

  // MoveOps.
  void move(const Place &dst, const Place &src) override;
  unsigned cycleScratch(bool fp) override { return fp ? F2 : S2; }

  // ---- emission ----
  void layout();
  void hints();
  bool sretFn() {
    Type rt = fn.getFunctionType().getReturnType();
    return !isa<LL::LLVMVoidType>(rt) && returnsInMemory(rt);
  }
  void prologue();
  void epilogue();
  void emit(uint32_t i);
  bool emitPair(const Inst &x, const Inst &y);
  void binop(const Inst &in);
  void cmpFlags(const Inst &c);
  void call(const Inst &in);
  void ret(const Inst &in);
  void edgeMoves(uint32_t e);
  void branchTo(uint32_t b) {
    fixups.push_back({here(), b, Fixup::B, AL, 0});
    e(a64::b(0));
  }
  void jumpIf(bool flags, Cond cc, unsigned reg, bool sense, uint32_t b) {
    if (flags)
      fixups.push_back({here(), b, Fixup::BCond, sense ? cc : Cond(cc ^ 1), 0});
    else
      fixups.push_back({here(), b, sense ? Fixup::Cbnz : Fixup::Cbz, AL, reg});
    e(0);
  }
  void addressOf(unsigned d, uint32_t sym, int64_t off);
  void copyMem(unsigned db, int64_t doff, unsigned sb, int64_t soff, uint64_t n);
  void zeroMem(unsigned base, int64_t off, uint64_t n);
};

void Emitter::addConst(unsigned rd, unsigned rn, int64_t off) {
  uint64_t m = off < 0 ? uint64_t(-off) : uint64_t(off);
  if (off == 0) {
    if (rd != rn) e(rn == SP || rd == SP ? addImm(rd, rn, 0) : movReg(rd, rn));
    return;
  }
  if (m >= (uint64_t(1) << 24)) {
    unsigned t = rd == rn || rn == SP ? SA : rd;
    if (t == rn) unsupported("mir: offset too large");
    movImm(t, uint64_t(off));
    if (rn == SP) {
      e(addImm(rd, SP, 0));
      e(addReg(rd, rd, t));
    } else {
      e(addReg(rd, rn, t));
    }
    return;
  }
  uint32_t hi = uint32_t(m >> 12), lo = uint32_t(m & 0xFFF);
  unsigned from = rn;
  if (hi) {
    e(off < 0 ? subImm(rd, from, hi, true, true) : addImm(rd, from, hi, true, true));
    from = rd;
  }
  if (lo || from != rd) e(off < 0 ? subImm(rd, from, lo) : addImm(rd, from, lo));
}

void Emitter::movImm(unsigned rd, uint64_t v) {
  if (v == 0) {
    e(movz(rd, 0, 0));
    return;
  }
  // movn for values with more 0xFFFF halfwords than zero ones.
  unsigned ones = 0, zeros = 0;
  for (unsigned hw = 0; hw < 4; hw++) {
    uint32_t h = uint32_t(v >> (16 * hw)) & 0xFFFF;
    ones += h == 0xFFFF;
    zeros += h == 0;
  }
  if (ones > zeros) {
    bool first = true;
    for (unsigned hw = 0; hw < 4; hw++) {
      uint32_t h = uint32_t(v >> (16 * hw)) & 0xFFFF;
      if (h == 0xFFFF) continue;
      e(first ? movn(rd, ~h & 0xFFFF, hw) : movk(rd, h, hw));
      first = false;
    }
    if (first) e(movn(rd, 0, 0));
    return;
  }
  bool first = true;
  for (unsigned hw = 0; hw < 4; hw++) {
    uint32_t h = uint32_t(v >> (16 * hw)) & 0xFFFF;
    if (!h) continue;
    e(first ? movz(rd, h, hw) : movk(rd, h, hw));
    first = false;
  }
}

void Emitter::mem(bool load, bool fp, unsigned rt, unsigned base, int64_t off, unsigned size) {
  unsigned ls = log2Size(size);
  if (off >= 0 && off % int64_t(size) == 0 && (off >> ls) < 4096) {
    if (fp)
      e(load ? ldrFImm(rt, base, uint32_t(off), ls) : strFImm(rt, base, uint32_t(off), ls));
    else
      e(load ? ldrImm(rt, base, uint32_t(off), ls) : strImm(rt, base, uint32_t(off), ls));
    return;
  }
  if (off >= -256 && off < 256) {
    if (fp)
      e(load ? ldurF(rt, base, int32_t(off), ls) : sturF(rt, base, int32_t(off), ls));
    else
      e(load ? ldur(rt, base, int32_t(off), ls) : stur(rt, base, int32_t(off), ls));
    return;
  }
  if (base == SA) unsupported("mir: address through the address scratch");
  addConst(SA, base, off);
  if (fp)
    e(load ? ldrFImm(rt, SA, 0, ls) : strFImm(rt, SA, 0, ls));
  else
    e(load ? ldrImm(rt, SA, 0, ls) : strImm(rt, SA, 0, ls));
}

void Emitter::remat(unsigned r, VId v) {
  const Inst &d = defOf(v);
  if (d.op == Op::Const)
    movImm(r, uint64_t(d.imm));
  else  // Frame
    addConst(r, SP, int64_t(objOff[d.aux]) + d.imm);
}

unsigned Emitter::gp(VId v, unsigned scratch) {
  const Loc &l = al.loc[v];
  switch (l.k) {
  case Loc::Reg: return l.reg;
  case Loc::Slot: ld(scratch, SP, int64_t(slotOff(l.slot)), 8); return scratch;
  case Loc::Remat: {
    uint64_t bits;
    if (konst(v, bits) && bits == 0) return XZR;
    remat(scratch, v);
    return scratch;
  }
  default: unsupported("mir: value without a place");
  }
}

unsigned Emitter::fr(VId v, unsigned scratch) {
  const Loc &l = al.loc[v];
  switch (l.k) {
  case Loc::Reg: return l.reg;
  case Loc::Slot: mem(true, true, scratch, SP, int64_t(slotOff(l.slot)), dbl(v) ? 8 : 4); return scratch;
  case Loc::Remat: {
    uint64_t bits;
    konst(v, bits);
    if (bits == 0) {
      e(fmovFromGp(scratch, XZR, dbl(v)));
    } else {
      movImm(S0, bits);
      e(fmovFromGp(scratch, S0, dbl(v)));
    }
    return scratch;
  }
  default: unsupported("mir: value without a place");
  }
}

unsigned Emitter::gpSigned(VId v, unsigned wdt, unsigned scratch) {
  unsigned r = gp(v, scratch);
  if (wdt >= 64 || r == XZR) return r;
  e(sbfx0(scratch, r, wdt));
  return scratch;
}

void Emitter::finG(VId v, unsigned r) {
  if (v == None) return;
  const Loc &l = al.loc[v];
  if (l.k == Loc::Reg) {
    if (l.reg != r) e(movReg(l.reg, r));
  } else if (l.k == Loc::Slot) {
    st(r, SP, int64_t(slotOff(l.slot)), 8);
  }
}

void Emitter::finF(VId v, unsigned r) {
  if (v == None) return;
  const Loc &l = al.loc[v];
  if (l.k == Loc::Reg) {
    if (l.reg != r) e(fmovReg(l.reg, r, dbl(v)));
  } else if (l.k == Loc::Slot) {
    mem(false, true, r, SP, int64_t(slotOff(l.slot)), dbl(v) ? 8 : 4);
  }
}

Place Emitter::place(VId v) const {
  Place p;
  if (v == None) return p;
  const Loc &l = al.loc[v];
  p.fp = isFp(v);
  switch (l.k) {
  case Loc::Reg:
    p.k = Place::Reg;
    p.reg = l.reg;
    break;
  case Loc::Slot:
    p.k = Place::Slot;
    p.slot = l.slot;
    break;
  case Loc::Remat:
    p.k = Place::Remat;
    p.inst = f.vals[v].def;
    break;
  default: break;
  }
  return p;
}

void Emitter::move(const Place &dst, const Place &src) {
  // A source into register r of dst's class.
  auto load = [&](unsigned r, bool fp) {
    switch (src.k) {
    case Place::Reg:
      if (src.fp == fp) {
        if (src.reg != r) e(fp ? fmovReg(r, src.reg, true) : movReg(r, src.reg));
      } else {
        e(fp ? fmovFromGp(r, src.reg, true) : fmovToGp(r, src.reg, true));
      }
      break;
    case Place::Slot: mem(true, fp, r, SP, int64_t(slotOff(src.slot)), 8); break;
    case Place::Mem: mem(true, fp, r, src.base, src.off, src.size); break;
    case Place::Remat: {
      const Inst &d = f.insts[src.inst];
      if (fp) {
        if (d.imm == 0) {
          e(fmovFromGp(r, XZR, true));
        } else {
          movImm(S1, uint64_t(d.imm));
          e(fmovFromGp(r, S1, true));
        }
      } else if (d.op == Op::Const) {
        movImm(r, uint64_t(d.imm));
      } else {
        addConst(r, SP, int64_t(objOff[d.aux]) + d.imm);
      }
      break;
    }
    default: break;
    }
  };
  switch (dst.k) {
  case Place::Reg: load(dst.reg, dst.fp); return;
  case Place::Slot:
  case Place::Mem: {
    bool fp = src.fp;
    unsigned r;
    if (src.k == Place::Reg) {
      r = src.reg;
    } else {
      r = fp ? F1 : S1;
      load(r, fp);
    }
    if (dst.k == Place::Slot)
      mem(false, fp, r, SP, int64_t(slotOff(dst.slot)), 8);
    else
      mem(false, fp, r, dst.base, dst.off, dst.size);
    return;
  }
  default: return;
  }
}

void Emitter::layout() {
  uint64_t out = 0;
  for (const CallRec &c : f.calls) {
    uint64_t n = 0;
    assignArgs(cast<LL::CallOp>(c.op).getArgOperands().getTypes(), n);
    out = std::max(out, n);
  }
  frameSize = (out + 15) & ~uint64_t(15);
  for (unsigned r = 19; r <= 28; r++)
    if (al.calleeGpr >> r & 1) savedGpr.push_back(r);
  for (unsigned r = 8; r <= 15; r++)
    if (al.calleeFpr >> r & 1) savedFpr.push_back(r);
  saveArea = frameSize;
  frameSize += 8 * (savedGpr.size() + savedFpr.size());
  slotBase = frameSize;
  frameSize += 8 * uint64_t(al.slots);
  auto rt = fn.getFunctionType().getReturnType();
  if (!isa<LL::LLVMVoidType>(rt) && returnsInMemory(rt)) {
    sret = true;
    sretSave = frameSize;
    frameSize += 8;
  }
  for (const FrameObj &o : f.objs) {
    frameSize = (frameSize + o.align - 1) & ~(o.align - 1);
    objOff.push_back(frameSize);
    frameSize += o.size;
  }
  frameSize = (frameSize + 15) & ~uint64_t(15);
  if (frameSize >= (uint64_t(1) << 24)) unsupported("frame larger than 16 MiB");
}

void Emitter::prologue() {
  e(stpPre(FP, LR, SP, -16));
  e(movSp(FP, SP));
  if (frameSize) {
    if (frameSize < 4096) {
      e(subImm(SP, SP, uint32_t(frameSize)));
    } else {
      e(subImm(SP, SP, uint32_t(frameSize >> 12), true, true));
      if (frameSize & 0xFFF) e(subImm(SP, SP, uint32_t(frameSize & 0xFFF)));
    }
  }
  uint64_t at = saveArea;
  for (size_t i = 0; i < savedGpr.size(); i += 2) {
    if (i + 1 < savedGpr.size()) {
      if (at + 8 <= 504) {
        e(stpOff(savedGpr[i], savedGpr[i + 1], SP, int32_t(at)));
      } else {
        st(savedGpr[i], SP, int64_t(at), 8);
        st(savedGpr[i + 1], SP, int64_t(at + 8), 8);
      }
      at += 16;
    } else {
      st(savedGpr[i], SP, int64_t(at), 8);
      at += 8;
    }
  }
  for (unsigned r : savedFpr) {
    mem(false, true, r, SP, int64_t(at), 8);
    at += 8;
  }
  if (sret) st(X8, SP, int64_t(sretSave), 8);
  // Incoming arguments, piece by piece, to their places.
  uint64_t stackSize = 0;
  std::vector<ArgLoc> locs = assignArgs(TypeRange(fn.getFunctionType().getParams()), stackSize);
  if (locs.size() != f.params.size()) unsupported("mir: parameter pieces");
  std::vector<Move> moves;
  for (size_t i = 0; i < locs.size(); i++) {
    const ArgLoc &l = locs[i];
    VId v = f.params[i];
    if (al.loc[v].k == Loc::None) continue;  // unused
    Place src;
    src.fp = l.fp;
    if (l.reg >= 0) {
      src.k = Place::Reg;
      src.reg = uint8_t(l.reg);
    } else {
      src.k = Place::Mem;
      src.base = FP;
      src.off = int64_t(16 + l.stack);
      src.size = uint8_t(sizeOf(l.t));
    }
    moves.push_back({place(v), src});
  }
  parallelMove(moves, *this);
  // Narrow integers arrive with their other bits undefined.
  for (size_t i = 0; i < locs.size(); i++) {
    VId v = f.params[i];
    if (locs[i].fp || al.loc[v].k == Loc::None || f.vals[v].width >= 64) continue;
    unsigned r = gp(v, S0);
    e(ubfx0(r, r, f.vals[v].width));
    finG(v, r);
  }
}

void Emitter::epilogue() {
  if (epiAt >= 0) {
    e(a64::b(int32_t(epiAt - int64_t(here()))));
    return;
  }
  epiAt = int64_t(here());
  uint64_t at = saveArea;
  for (size_t i = 0; i < savedGpr.size(); i += 2) {
    if (i + 1 < savedGpr.size()) {
      if (at + 8 <= 504) {
        e(ldpOff(savedGpr[i], savedGpr[i + 1], SP, int32_t(at)));
      } else {
        ld(savedGpr[i], SP, int64_t(at), 8);
        ld(savedGpr[i + 1], SP, int64_t(at + 8), 8);
      }
      at += 16;
    } else {
      ld(savedGpr[i], SP, int64_t(at), 8);
      at += 8;
    }
  }
  for (unsigned r : savedFpr) {
    mem(true, true, r, SP, int64_t(at), 8);
    at += 8;
  }
  if (frameSize) e(movSp(SP, FP));
  e(ldpPost(FP, LR, SP, 16));
  e(a64::ret());
}

void Emitter::addressOf(unsigned d, uint32_t sym, int64_t off) {
  const std::string &name = f.syms[sym];
  unsigned s = w.symbol(machoName(name));
  uint64_t at = here();
  e(adrp(d));
  if (mg.isDefinedData(name)) {
    w.reloc(Sect::Text, at, Reloc::Page21, s);
    at = here();
    e(addImm(d, d, 0));
    w.reloc(Sect::Text, at, Reloc::PageOff12, s);
  } else {
    w.reloc(Sect::Text, at, Reloc::GotPage21, s);
    at = here();
    e(ldrImm(d, d, 0, 3));
    w.reloc(Sect::Text, at, Reloc::GotPageOff12, s);
  }
  if (off) addConst(d, d, off);
}

// Copies n bytes (no overlap) 16 at a time where the offsets allow, through
// S1 and S2.
void Emitter::copyMem(unsigned db, int64_t doff, unsigned sb, int64_t soff, uint64_t n) {
  if (doff < 0 || soff < 0 || doff + int64_t(n) > 4096 || soff + int64_t(n) > 4096) {
    // Through S0 (destination, made first) and SA (source); db is never SA
    // and sb never S0 (addrOf's scratch registers are SC and SA).
    addConst(S0, db, doff);
    addConst(SA, sb, soff);
    db = S0;
    sb = SA;
    doff = soff = 0;
  }
  uint64_t i = 0;
  for (; i + 16 <= n && (doff + int64_t(i)) % 8 == 0 && (soff + int64_t(i)) % 8 == 0 && doff + int64_t(i) + 8 <= 504 &&
         soff + int64_t(i) + 8 <= 504;
       i += 16) {
    e(ldpOff(S1, S2, sb, int32_t(soff + int64_t(i))));
    e(stpOff(S1, S2, db, int32_t(doff + int64_t(i))));
  }
  for (unsigned sz = 8; sz >= 1; sz /= 2)
    for (; i + sz <= n; i += sz) {
      ld(S1, sb, soff + int64_t(i), sz);
      st(S1, db, doff + int64_t(i), sz);
    }
}

void Emitter::zeroMem(unsigned base, int64_t off, uint64_t n) {
  if (off < 0 || off + int64_t(n) > 4096) {
    addConst(S0, base, off);  // base is S1, sp or a value's register
    base = S0;
    off = 0;
  }
  uint64_t i = 0;
  for (; i + 16 <= n && (off + int64_t(i)) % 8 == 0 && off + int64_t(i) + 8 <= 504; i += 16)
    e(stpOff(XZR, XZR, base, int32_t(off + int64_t(i))));
  for (unsigned sz = 8; sz >= 1; sz /= 2)
    for (; i + sz <= n; i += sz) st(XZR, base, off + int64_t(i), sz);
}

static Cond icmpCond(LL::ICmpPredicate p) {
  switch (p) {
  case LL::ICmpPredicate::eq: return EQ;
  case LL::ICmpPredicate::ne: return NE;
  case LL::ICmpPredicate::slt: return LT;
  case LL::ICmpPredicate::sle: return LE;
  case LL::ICmpPredicate::sgt: return GT;
  case LL::ICmpPredicate::sge: return GE;
  case LL::ICmpPredicate::ult: return LO;
  case LL::ICmpPredicate::ule: return LS;
  case LL::ICmpPredicate::ugt: return HI;
  case LL::ICmpPredicate::uge: return HS;
  }
  return AL;
}

void Emitter::cmpFlags(const Inst &c) {
  auto p = LL::ICmpPredicate(c.aux);
  bool sgn = isSignedPred(p);
  unsigned a = sgn ? gpSigned(c.a, c.w, S0) : gp(c.a, S0);
  uint64_t bits;
  if (konst(c.b, bits)) {
    int64_t v = sgn && c.w < 64 ? int64_t(bits << (64 - c.w)) >> (64 - c.w) : int64_t(bits);
    if (v >= 0 && v < 4096 && (sgn || bits < 4096)) {
      e(cmpImm(a, uint32_t(v)));
      return;
    }
  }
  unsigned b = sgn ? gpSigned(c.b, c.w, S1) : gp(c.b, S1);
  e(cmpReg(a, b));
}

void Emitter::binop(const Inst &in) {
  unsigned d = dstG(in.dst, S0);
  uint64_t k;
  bool ck = konst(in.b, k);
  // Immediate forms: add/sub of 12 bits (either sign, shifted by 12 when
  // the low bits are zero), logical immediates.
  if (ck && (in.op == Op::Add || in.op == Op::Sub)) {
    int64_t v = in.w < 64 ? int64_t(k << (64 - in.w)) >> (64 - in.w) : int64_t(k);  // as the width sees it
    if (in.op == Op::Sub) v = -v;
    uint64_t m = v < 0 ? uint64_t(-v) : uint64_t(v);
    bool lo = m < 4096, hi = (m & 0xFFF) == 0 && m < (uint64_t(1) << 24);
    if (lo || hi) {
      unsigned a = gp(in.a, S0);
      uint32_t imm = uint32_t(hi && !lo ? m >> 12 : m);
      e(v < 0 ? subImm(d, a, imm, true, hi && !lo) : addImm(d, a, imm, true, hi && !lo));
      norm(d, in.w);
      finG(in.dst, d);
      return;
    }
  }
  uint32_t enc;
  if (ck && (in.op == Op::And || in.op == Op::Or || in.op == Op::Xor) && logicalImm(k, enc)) {
    unsigned a = gp(in.a, S0);
    e(in.op == Op::And ? andImm(d, a, enc) : in.op == Op::Or ? orrImm(d, a, enc) : eorImm(d, a, enc));
    norm(d, in.w);
    finG(in.dst, d);
    return;
  }
  if (ck && (in.op == Op::Shl || in.op == Op::LShr || in.op == Op::AShr) && (k & 63)) {
    unsigned s = unsigned(k & 63);
    unsigned a = in.op == Op::AShr ? gpSigned(in.a, in.w, S0) : gp(in.a, S0);
    e(in.op == Op::Shl ? lslImm(d, a, s) : in.op == Op::LShr ? lsrImm(d, a, s) : asrImm(d, a, s));
    norm(d, in.w);
    finG(in.dst, d);
    return;
  }
  bool sgn = in.op == Op::SDiv || in.op == Op::SRem || in.op == Op::AShr;
  unsigned a = sgn ? gpSigned(in.a, in.w, S0) : gp(in.a, S0);
  unsigned b = (in.op == Op::SDiv || in.op == Op::SRem) ? gpSigned(in.b, in.w, S1) : gp(in.b, S1);
  switch (in.op) {
  case Op::Add: e(addReg(d, a, b)); break;
  case Op::Sub: e(subReg(d, a, b)); break;
  case Op::Mul: e(mul(d, a, b)); break;
  case Op::And: e(andReg(d, a, b)); break;
  case Op::Or: e(orrReg(d, a, b)); break;
  case Op::Xor: e(eorReg(d, a, b)); break;
  case Op::Shl: e(lslv(d, a, b)); break;
  case Op::LShr: e(lsrv(d, a, b)); break;
  case Op::AShr: e(asrv(d, a, b)); break;
  case Op::UDiv: e(udiv(d, a, b)); break;
  case Op::SDiv: e(sdiv(d, a, b)); break;
  case Op::URem:
    e(udiv(S2, a, b));
    e(msub(d, S2, b, a));
    break;
  case Op::SRem:
    e(sdiv(S2, a, b));
    e(msub(d, S2, b, a));
    break;
  case Op::UMulH: e(umulh(d, a, b)); break;
  default: unsupported("mir: binop");
  }
  norm(d, in.w);
  finG(in.dst, d);
}

void Emitter::edgeMoves(uint32_t ei) {
  const Edge &ed = f.edges[ei];
  const BB &to = f.blocks[ed.block];
  std::vector<Move> moves;
  for (uint32_t i = ed.argBegin; i < ed.argEnd; i++) {
    VId p = to.params[i - ed.argBegin];
    if (al.loc[p].k == Loc::None) continue;
    moves.push_back({place(p), place(f.edgeArgs[i])});
  }
  if (!moves.empty()) parallelMove(moves, *this);
}

void Emitter::call(const Inst &in) {
  const CallRec &c = f.calls[in.extra];
  auto op = cast<LL::CallOp>(c.op);
  auto args = op.getArgOperands();
  uint64_t stackSize = 0;
  std::vector<ArgLoc> locs = assignArgs(args.getTypes(), stackSize);
  if (locs.size() != c.args.size()) unsupported("mir: call pieces");
  std::vector<Move> moves;
  std::vector<std::pair<unsigned, unsigned>> sext;  // (register, width)
  for (size_t i = 0; i < locs.size(); i++) {
    const ArgLoc &l = locs[i];
    Place d;
    d.fp = l.fp;
    if (l.reg >= 0) {
      d.k = Place::Reg;
      d.reg = uint8_t(l.reg);
    } else {
      d.k = Place::Mem;
      d.base = SP;
      d.off = int64_t(l.stack);
      d.size = uint8_t(sizeOf(l.t));
    }
    bool signExt = !l.fp && intWidth(l.t) < 64 && mg.signExtArg(op, l.arg);
    if (signExt) {
      if (l.reg < 0) unsupported("mir: sign extended stack argument");
      sext.push_back({unsigned(l.reg), intWidth(l.t)});
    }
    moves.push_back({d, place(c.args[i])});
  }
  if (c.indirect) moves.push_back({Place::reg_(SC, false), place(c.callee)});
  parallelMove(moves, *this);
  for (auto [r, wd] : sext) e(sbfx0(r, r, wd));
  if (c.sretObj >= 0) addConst(X8, SP, int64_t(objOff[size_t(c.sretObj)]));
  if (c.indirect) {
    e(blr(SC));
  } else {
    uint64_t at = here();
    e(bl(0));
    w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol(machoName(f.syms[c.sym])));
  }
  if (c.results.empty()) return;
  std::vector<std::pair<uint64_t, Type>> parts;
  flatten(op.getResult().getType(), 0, parts);
  if (parts.size() != c.results.size()) unsupported("mir: result pieces");
  unsigned ngp = 0, nfp = 0;
  moves.clear();
  for (size_t i = 0; i < parts.size(); i++) {
    Type t = parts[i].second;
    bool fp = isFloat(t);
    unsigned r = fp ? nfp++ : ngp++;
    if (r >= 8) unsupported("result returned in memory");
    VId v = c.results[i];
    if (al.loc[v].k == Loc::None) continue;
    if (!fp && intWidth(t) < 64) e(ubfx0(r, r, intWidth(t)));  // the other bits are undefined
    moves.push_back({place(v), Place::reg_(r, fp)});
  }
  parallelMove(moves, *this);
}

void Emitter::ret(const Inst &in) {
  const RetRec &r = f.rets[in.extra];
  if (!r.pieces.empty()) {
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(fn.getFunctionType().getReturnType(), 0, parts);
    if (parts.size() != r.pieces.size()) unsupported("mir: return pieces");
    if (sret) {
      ld(SA, SP, int64_t(sretSave), 8);
      for (size_t i = 0; i < parts.size(); i++) {
        auto [off, t] = parts[i];
        VId v = r.pieces[i];
        if (isFloat(t))
          mem(false, true, fr(v, F0), SA, int64_t(off), unsigned(sizeOf(t)));
        else
          st(gp(v, S0), SA, int64_t(off), unsigned(sizeOf(t)));
      }
    } else {
      std::vector<Move> moves;
      std::vector<std::pair<unsigned, unsigned>> sext;
      unsigned ngp = 0, nfp = 0;
      bool signExt = ModuleGen::signExtResult(fn);
      for (size_t i = 0; i < parts.size(); i++) {
        Type t = parts[i].second;
        bool fp = isFloat(t);
        unsigned rg = fp ? nfp++ : ngp++;
        if (rg >= 8) unsupported("result returned in memory");
        moves.push_back({Place::reg_(rg, fp), place(r.pieces[i])});
        if (!fp && signExt && intWidth(t) < 64) sext.push_back({rg, intWidth(t)});
      }
      parallelMove(moves, *this);
      for (auto [rg, wd] : sext) e(sbfx0(rg, rg, wd));
    }
  }
  epilogue();
}

// Two 8-byte integer loads or stores at base + k and base + k + 8 (the same
// base value): one ldp / stp.
bool Emitter::emitPair(const Inst &x, const Inst &y) {
  if (x.op != y.op || (x.op != Op::Load && x.op != Op::Store)) return false;
  if (x.aux != 8 || y.aux != 8 || (x.flags | y.flags) || x.a != y.a || y.imm != x.imm + 8) return false;
  if (x.imm % 8 || x.imm < -512 || x.imm > 496) return false;
  if (x.op == Op::Load) {
    if (isFp(x.dst) || isFp(y.dst) || x.w == 1 || y.w == 1) return false;
    if (al.loc[x.dst].k == Loc::None || al.loc[y.dst].k == Loc::None) return false;
    auto [base, off] = addrOf(x.a, x.imm, SA);
    if (off % 8 || off < -512 || off > 496) return false;
    unsigned d1 = dstG(x.dst, S0), d2 = dstG(y.dst, S1);
    if (d1 == d2) return false;
    e(ldpOff(d1, d2, base, int32_t(off)));
    finG(x.dst, d1);
    finG(y.dst, d2);
    return true;
  }
  if (isFp(x.b) || isFp(y.b)) return false;
  auto [base, off] = addrOf(x.a, x.imm, SA);
  if (off % 8 || off < -512 || off > 496) return false;
  unsigned v1 = gp(x.b, S0), v2 = gp(y.b, S1);
  e(stpOff(v1, v2, base, int32_t(off)));
  return true;
}

void Emitter::emit(uint32_t i) {
  const Inst &in = f.insts[i];
  switch (in.op) {
  case Op::Const:
  case Op::Frame:
    return;  // made at each use
  case Op::Addr: {
    unsigned d = dstG(in.dst, S0);
    addressOf(d, in.sym, in.imm);
    finG(in.dst, d);
    return;
  }
  case Op::Copy: {
    if (isFp(in.dst)) {
      unsigned d = dstF(in.dst, F0);
      unsigned s = fr(in.a, F0);
      if (d != s) e(fmovReg(d, s, dbl(in.dst)));
      finF(in.dst, d);
    } else {
      unsigned d = dstG(in.dst, S0);
      unsigned s = gp(in.a, S0);
      if (d != s) e(movReg(d, s));
      finG(in.dst, d);
    }
    return;
  }
  case Op::Add: case Op::Sub: case Op::Mul: case Op::UDiv: case Op::SDiv: case Op::URem: case Op::SRem:
  case Op::And: case Op::Or: case Op::Xor: case Op::Shl: case Op::LShr: case Op::AShr: case Op::UMulH:
    binop(in);
    return;
  case Op::ICmp: {
    cmpFlags(in);
    unsigned d = dstG(in.dst, S0);
    e(cset(d, icmpCond(LL::ICmpPredicate(in.aux))));
    finG(in.dst, d);
    return;
  }
  case Op::Select: {
    unsigned c = gp(in.a, S2);
    e(cmpImm(c, 0));
    if (isFp(in.dst)) {
      unsigned x = fr(in.b, F0), y = fr(in.c, F1), d = dstF(in.dst, F0);
      e(fcsel(d, x, y, NE, dbl(in.dst)));
      finF(in.dst, d);
    } else {
      unsigned x = gp(in.b, S0), y = gp(in.c, S1), d = dstG(in.dst, S0);
      e(csel(d, x, y, NE));
      finG(in.dst, d);
    }
    return;
  }
  case Op::SExt: {
    unsigned a = gp(in.a, S0), d = dstG(in.dst, S0);
    e(sbfx0(d, a, in.aux));
    norm(d, in.w);
    finG(in.dst, d);
    return;
  }
  case Op::Trunc: {
    unsigned a = gp(in.a, S0), d = dstG(in.dst, S0);
    e(ubfx0(d, a, in.w));
    finG(in.dst, d);
    return;
  }
  case Op::FAdd: case Op::FSub: case Op::FMul: case Op::FDiv: case Op::FMin: case Op::FMax: case Op::FMinNum:
  case Op::FMaxNum: {
    bool x = dbl(in.dst);
    unsigned a = fr(in.a, F0), b = fr(in.b, F1), d = dstF(in.dst, F0);
    switch (in.op) {
    case Op::FAdd: e(fadd(d, a, b, x)); break;
    case Op::FSub: e(fsub(d, a, b, x)); break;
    case Op::FMul: e(fmul(d, a, b, x)); break;
    case Op::FDiv: e(fdiv(d, a, b, x)); break;
    case Op::FMin: e(fmin(d, a, b, x)); break;
    case Op::FMax: e(fmax(d, a, b, x)); break;
    case Op::FMinNum: e(fminnm(d, a, b, x)); break;
    default: e(fmaxnm(d, a, b, x)); break;
    }
    finF(in.dst, d);
    return;
  }
  case Op::FNeg: {
    unsigned a = fr(in.a, F0), d = dstF(in.dst, F0);
    e(fneg(d, a, dbl(in.dst)));
    finF(in.dst, d);
    return;
  }
  case Op::FMA:
  case Op::FMulAdd: {
    unsigned a = fr(in.a, F0), b = fr(in.b, F1), c = fr(in.c, F2), d = dstF(in.dst, F0);
    e(fmadd(d, a, b, c, dbl(in.dst)));
    finF(in.dst, d);
    return;
  }
  case Op::FCmp: {
    bool x = in.w == 64;
    unsigned a = fr(in.a, F0), b = fr(in.b, F1);
    e(fcmp(a, b, x));
    unsigned d = dstG(in.dst, S0);
    using P = LL::FCmpPredicate;
    auto one = [&](Cond k) { e(cset(d, k)); };
    auto either = [&](Cond k1, Cond k2) {
      e(cset(S1, k1));
      e(cset(S2, k2));
      e(orrReg(d, S1, S2));
    };
    switch (P(in.aux)) {
    case P::_false: e(movz(d, 0, 0)); break;
    case P::_true: e(movz(d, 1, 0)); break;
    case P::oeq: one(EQ); break;
    case P::ogt: one(GT); break;
    case P::oge: one(GE); break;
    case P::olt: one(MI); break;
    case P::ole: one(LS); break;
    case P::one: either(MI, GT); break;
    case P::ord: one(VC); break;
    case P::ueq: either(EQ, VS); break;
    case P::ugt: one(HI); break;
    case P::uge: one(PL); break;
    case P::ult: one(LT); break;
    case P::ule: one(LE); break;
    case P::une: one(NE); break;
    case P::uno: one(VS); break;
    }
    finG(in.dst, d);
    return;
  }
  case Op::SIToFP:
  case Op::UIToFP: {
    bool sg = in.op == Op::SIToFP;
    unsigned a = sg ? gpSigned(in.a, in.w, S0) : gp(in.a, S0);
    unsigned d = dstF(in.dst, F0);
    e(sg ? scvtf(d, a, dbl(in.dst)) : ucvtf(d, a, dbl(in.dst)));
    finF(in.dst, d);
    return;
  }
  case Op::FPToSI: case Op::FPToUI: case Op::FPToSISat: case Op::FPToUISat: {
    bool x = in.w == 64;
    unsigned a = fr(in.a, F0), d = dstG(in.dst, S0);
    bool sg = in.op == Op::FPToSI || in.op == Op::FPToSISat;
    e(sg ? fcvtzs(d, a, x) : fcvtzu(d, a, x));
    norm(d, f.vals[in.dst].width);
    finG(in.dst, d);
    return;
  }
  case Op::FPExt: {
    unsigned a = fr(in.a, F0), d = dstF(in.dst, F0);
    e(fcvtStoD(d, a));
    finF(in.dst, d);
    return;
  }
  case Op::FPTrunc: {
    unsigned a = fr(in.a, F0), d = dstF(in.dst, F0);
    e(fcvtDtoS(d, a));
    finF(in.dst, d);
    return;
  }
  case Op::Bits: {
    if (isFp(in.dst)) {
      unsigned a = gp(in.a, S0), d = dstF(in.dst, F0);
      e(fmovFromGp(d, a, dbl(in.dst)));
      finF(in.dst, d);
    } else {
      unsigned a = fr(in.a, F0), d = dstG(in.dst, S0);
      e(fmovToGp(d, a, dbl(in.a)));  // a 32-bit fmov zeroes the upper half
      finG(in.dst, d);
    }
    return;
  }
  case Op::Ctlz: {
    unsigned a = gp(in.a, S0), d = dstG(in.dst, S0);
    e(clz(d, a));
    if (in.w < 64) e(subImm(d, d, 64 - in.w));
    finG(in.dst, d);
    return;
  }
  case Op::Cttz: {
    unsigned a = gp(in.a, S0), d = dstG(in.dst, S0);
    if (in.w < 64) {  // a zero input counts w
      movImm(S1, uint64_t(1) << in.w);
      e(orrReg(S0, a, S1));
      a = S0;
    }
    e(rbit(d, a));
    e(clz(d, d));
    finG(in.dst, d);
    return;
  }
  case Op::Ctpop: {
    unsigned a = gp(in.a, S0), d = dstG(in.dst, S0);
    e(fmovFromGp(F0, a, true));
    e(cnt8b(F0, F0));
    e(addv8b(F0, F0));
    e(fmovToGp(d, F0, true));
    finG(in.dst, d);
    return;
  }
  case Op::Bswap:
  case Op::Bitrev: {
    unsigned a = gp(in.a, S0), d = dstG(in.dst, S0);
    e(in.op == Op::Bswap ? rev(d, a) : rbit(d, a));
    if (in.w < 64) e(lsrImm(d, d, 64 - in.w));
    finG(in.dst, d);
    return;
  }
  case Op::Fshl:
  case Op::Fshr: {
    bool left = in.op == Op::Fshl;
    unsigned n = in.w;
    // Work in S0 (a), S1 (b), S2 (s), SA, SC.
    unsigned a = gp(in.a, S0);
    if (a != S0) e(movReg(S0, a));
    unsigned b = gp(in.b, S1);
    if (b != S1) e(movReg(S1, b));
    unsigned s = gp(in.c, S2);
    if (s != S2) e(movReg(S2, s));
    if (n == 64) {
      movImm(SA, 63);
      e(andReg(S2, S2, SA));
      e(eorReg(SA, S2, SA));  // 63 - s
      if (left) {
        e(lslv(S0, S0, S2));
        e(lsrImm(S1, S1, 1));
        e(lsrv(S1, S1, SA));
      } else {
        e(lsrv(S1, S1, S2));
        e(lslImm(S0, S0, 1));
        e(lslv(S0, S0, SA));
      }
      e(orrReg(S0, S0, S1));
    } else {
      movImm(SA, n);
      e(udiv(SC, S2, SA));
      e(msub(S2, SC, SA, S2));  // s mod n
      e(lslv(S0, S0, SA));
      e(orrReg(S0, S0, S1));
      if (left) {
        e(lslv(S0, S0, S2));
        e(lsrv(S0, S0, SA));
      } else {
        e(lsrv(S0, S0, S2));
      }
      norm(S0, n);
    }
    finG(in.dst, S0);
    return;
  }
  case Op::FrameAddr: {
    unsigned d = dstG(in.dst, S0);
    e(movSp(d, FP));
    finG(in.dst, d);
    return;
  }
  case Op::Load: {
    unsigned size = in.aux;
    if (in.dst == None || al.loc[in.dst].k == Loc::None) {
      if (!(in.flags & Atomic)) return;  // nobody reads it (only volatile loads stay; none here)
    }
    auto [base, off] = addrOf(in.a, (in.flags & Atomic) ? 0 : in.imm, SA);
    if ((in.flags & Atomic) && base == SP) {
      addConst(SA, SP, off);
      base = SA;
      off = 0;
    }
    if (isFp(in.dst)) {
      unsigned d = dstF(in.dst, F0);
      mem(true, true, d, base, off, size);
      finF(in.dst, d);
      return;
    }
    unsigned d = dstG(in.dst, S0);
    if (in.flags & Atomic)
      e(ldar(d, base, log2Size(size)));
    else
      ld(d, base, off, size);
    if (in.w == 1) e(ubfx0(d, d, 1));
    finG(in.dst, d);
    return;
  }
  case Op::Store: {
    unsigned size = in.aux;
    auto [base, off] = addrOf(in.a, (in.flags & Atomic) ? 0 : in.imm, SA);
    if ((in.flags & Atomic) && base == SP) {
      addConst(SA, SP, off);
      base = SA;
      off = 0;
    }
    if (isFp(in.b)) {
      mem(false, true, fr(in.b, F0), base, off, size);
      return;
    }
    unsigned v = gp(in.b, S0);
    if (in.flags & Atomic)
      e(stlr(v, base, log2Size(size)));
    else
      st(v, base, off, size);
    return;
  }
  case Op::Zero: {
    auto [base, off] = addrOf(in.a, in.imm, S1);
    zeroMem(base, off, in.aux2);
    return;
  }
  case Op::Copy8: {
    auto [db, doff] = addrOf(in.a, in.imm, SC);
    auto [sb, soff] = addrOf(in.b, in.imm2, SA);
    copyMem(db, doff, sb, soff, in.aux2);
    return;
  }
  case Op::MemCall: {
    std::vector<Move> moves{{Place::reg_(0, false), place(in.a)},
                            {Place::reg_(1, false), place(in.b)},
                            {Place::reg_(2, false), place(in.c)}};
    parallelMove(moves, *this);
    uint64_t at = here();
    e(bl(0));
    w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol(in.aux ? "_memset" : "_memmove"));
    return;
  }
  case Op::Call: call(in); return;
  case Op::AtomicRMW: {
    unsigned ls = log2Size((in.w + 7) / 8 >= 8 ? 8 : in.w <= 8 ? 1 : in.w <= 16 ? 2 : in.w <= 32 ? 4 : 8);
    unsigned p = gp(in.a, S1);
    if (p != S1) e(movReg(S1, p));
    unsigned v = gp(in.b, S2);
    if (v != S2) e(movReg(S2, v));
    using B = LL::AtomicBinOp;
    switch (B(in.aux)) {
    case B::add: e(ldaddal(S2, S0, S1, ls)); break;
    case B::sub:
      e(neg(S2, S2));
      e(ldaddal(S2, S0, S1, ls));
      break;
    case B::_and:
      e(mvn(S2, S2));
      e(ldclral(S2, S0, S1, ls));
      break;
    case B::_or: e(ldsetal(S2, S0, S1, ls)); break;
    case B::_xor: e(ldeoral(S2, S0, S1, ls)); break;
    case B::xchg: e(swpal(S2, S0, S1, ls)); break;
    default: unsupported("atomicrmw operation");
    }
    norm(S0, in.w);
    finG(in.dst, S0);
    return;
  }
  case Op::CmpXchg: {
    unsigned ls = log2Size(in.w <= 8 ? 1 : in.w <= 16 ? 2 : in.w <= 32 ? 4 : 8);
    unsigned p = gp(in.a, S1);
    if (p != S1) e(movReg(S1, p));
    unsigned c = gp(in.b, S0);
    if (c != S0) e(movReg(S0, c));
    e(movReg(SC, S0));
    unsigned v = gp(in.c, S2);
    if (v != S2) e(movReg(S2, v));
    e(casal(S0, S2, S1, ls));  // S0 <- old
    norm(S0, in.w);
    norm(SC, in.w);
    e(cmpReg(S0, SC));
    finG(in.dst, S0);
    unsigned ok = dstG(VId(in.extra), S1);
    e(cset(ok, EQ));
    finG(VId(in.extra), ok);
    return;
  }
  case Op::Fence: e(dmbIsh()); return;
  case Op::Unreachable: e(brk(1)); return;
  case Op::Ret: ret(in); return;
  case Op::Br: {
    edgeMoves(in.extra);
    uint32_t t = f.edges[in.extra].block;
    if (t != nextBlock) branchTo(t);
    return;
  }
  case Op::CondBr: {
    bool flags = false, inv = false;
    Cond cc = AL;
    unsigned cr = 0;
    const Val &cv = f.vals[in.a];
    if (cv.def != ~0u && al.fused[cv.def]) {
      const Inst &ic = f.insts[cv.def];
      auto p = LL::ICmpPredicate(ic.aux);
      uint64_t bits;
      if ((p == LL::ICmpPredicate::eq || p == LL::ICmpPredicate::ne) && konst(ic.b, bits) && bits == 0) {
        cr = gp(ic.a, S0);
        inv = p == LL::ICmpPredicate::eq;
      } else {
        cmpFlags(ic);
        flags = true;
        cc = icmpCond(p);
      }
    } else {
      cr = gp(in.a, S0);
    }
    const Edge &te = f.edges[in.extra], &fe = f.edges[in.aux2];
    uint32_t t = te.block, fb = fe.block;
    bool targs = te.argEnd > te.argBegin, fargs = fe.argEnd > fe.argBegin;
    if (!targs && !fargs && t == nextBlock) {
      jumpIf(flags, cc, cr, inv, fb);
    } else if (!targs) {
      jumpIf(flags, cc, cr, !inv, t);
      edgeMoves(in.aux2);
      if (fb != nextBlock) branchTo(fb);
    } else if (!fargs) {
      jumpIf(flags, cc, cr, inv, fb);
      edgeMoves(in.extra);
      if (t != nextBlock) branchTo(t);
    } else {
      uint64_t at = here();
      e(0);
      edgeMoves(in.aux2);
      branchTo(fb);
      int32_t off = int32_t(here() - at);
      uint32_t word = flags ? bcond(cc, off) : inv ? cbz(cr, off) : cbnz(cr, off);
      std::memcpy(&w.bytes(Sect::Text)[at], &word, 4);
      edgeMoves(in.extra);
      if (t != nextBlock) branchTo(t);
    }
    return;
  }
  case Op::Switch: {
    const SwitchRec &sr = f.switches[in.extra];
    unsigned v = gp(in.a, S2);
    if (v != S2) e(movReg(S2, v));  // edge moves may write v's register
    for (size_t k = 0; k + 1 < sr.edges.size(); k++) {
      uint64_t cv = sr.values[k];
      if (cv < 4096) {
        e(cmpImm(S2, uint32_t(cv)));
      } else {
        movImm(S1, cv);
        e(cmpReg(S2, S1));
      }
      uint32_t ei = sr.edges[k];
      const Edge &ed = f.edges[ei];
      if (ed.argEnd == ed.argBegin) {
        jumpIf(true, EQ, 0, true, ed.block);
        continue;
      }
      uint64_t at = here();
      e(0);
      edgeMoves(ei);
      branchTo(ed.block);
      uint32_t word = bcond(NE, int32_t(here() - at));
      std::memcpy(&w.bytes(Sect::Text)[at], &word, 4);
    }
    uint32_t d = sr.edges.back();
    edgeMoves(d);
    if (f.edges[d].block != nextBlock) branchTo(f.edges[d].block);
    return;
  }
  default: unsupported("mir: operation");
  }
}

// Register hints: arguments, results, parameters and return values where
// the calling convention puts them.
void Emitter::hints() {
  al.hint.assign(f.vals.size(), -1);
  auto set = [&](VId v, int r) {
    if (v != None && r >= 0 && al.hint[v] < 0) al.hint[v] = int16_t(r);
  };
  uint64_t n = 0;
  std::vector<ArgLoc> pl = assignArgs(TypeRange(fn.getFunctionType().getParams()), n);
  for (size_t i = 0; i < pl.size() && i < f.params.size(); i++) set(f.params[i], pl[i].reg);
  auto results = [&](Type rt, const std::vector<VId> &vs) {
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(rt, 0, parts);
    unsigned ngp = 0, nfp = 0;
    for (size_t i = 0; i < parts.size() && i < vs.size(); i++) {
      Type t = parts[i].second;
      int r = isFloat(t) ? (nfp < 8 ? int(nfp) : -1) : (ngp < 8 ? int(ngp) : -1);
      isFloat(t) ? nfp++ : ngp++;
      set(vs[i], r);
    }
  };
  for (const CallRec &c : f.calls) {
    auto op = cast<LL::CallOp>(c.op);
    std::vector<ArgLoc> locs = assignArgs(op.getArgOperands().getTypes(), n);
    for (size_t i = 0; i < locs.size() && i < c.args.size(); i++) set(c.args[i], locs[i].reg);
    if (!c.results.empty()) results(op.getResult().getType(), c.results);
  }
  Type rt = fn.getFunctionType().getReturnType();
  if (!isa<LL::LLVMVoidType>(rt) && !sretFn())
    for (const RetRec &r : f.rets) results(rt, r.pieces);
}

// TIER0_MIR_TIME: time per phase over all functions, printed at exit.
struct PhaseTimes {
  std::atomic<uint64_t> check{0}, build{0}, alloc{0}, emit{0};
  ~PhaseTimes() {
    if (getenv("TIER0_MIR_TIME"))
      fprintf(stderr, "mir time: check %.1f ms, build %.1f ms, alloc %.1f ms, emit %.1f ms\n", check / 1e6, build / 1e6,
              alloc / 1e6, emit / 1e6);
  }
} phaseTimes;
static const bool timeOn = getenv("TIER0_MIR_TIME") != nullptr;
static uint64_t nowNs() {
  return uint64_t(std::chrono::duration_cast<std::chrono::nanoseconds>(std::chrono::steady_clock::now().time_since_epoch()).count());
}

void Emitter::run() {
  uint64_t t0 = timeOn ? nowNs() : 0;
  mg.checkFunction(fn);
  uint64_t t1 = timeOn ? nowNs() : 0;
  build(mg, fn, f, false);
  uint64_t t2 = timeOn ? nowNs() : 0;
  hints();
  allocate(f, regs(), al);
  uint64_t t3 = timeOn ? nowNs() : 0;
  if (timeOn) {
    phaseTimes.check += t1 - t0;
    phaseTimes.build += t2 - t1;
    phaseTimes.alloc += t3 - t2;
  }
  struct EmitTimer {
    uint64_t t;
    ~EmitTimer() {
      if (timeOn) phaseTimes.emit += nowNs() - t;
    }
  } et{t3};
  layout();
  fnStart = here();
  std::vector<ModuleGen::Row> rows;
  auto row = [&](Location l) {
    auto r = lineOf(l);
    if (!r) return;
    uint32_t file = mg.fileId(r->first);
    if (!rows.empty() && std::get<1>(rows.back()) == r->second && std::get<2>(rows.back()) == file) return;
    if (!rows.empty() && std::get<0>(rows.back()) == here() - fnStart) rows.pop_back();
    rows.push_back({here() - fnStart, r->second, file});
  };
  row(fn.getLoc());
  std::map<std::string, uint64_t> ob;
  uint64_t was = here();
  prologue();
  if (opStatsOn) ob["prologue"] += here() - was;
  blockStart.assign(f.blocks.size(), 0);
  for (uint32_t b = 0; b < f.blocks.size(); b++) {
    const BB &bl = f.blocks[b];
    if (bl.dead) continue;
    blockStart[b] = here();
    nextBlock = b + 1;
    while (nextBlock < f.blocks.size() && f.blocks[nextBlock].dead) nextBlock++;
    for (uint32_t i = bl.begin; i < bl.end; i++) {
      const Inst &in = f.insts[i];
      if (in.dead || al.fused[i]) continue;
      if (in.src) row(in.src->getLoc());
      was = here();
      uint32_t j = i + 1;
      while (j < bl.end && (f.insts[j].dead || al.fused[j])) j++;
      if (j < bl.end && emitPair(in, f.insts[j])) {
        i = j;
        if (opStatsOn) ob[std::string("mir.") + std::to_string(unsigned(in.op))] += here() - was;
        continue;
      }
      emit(i);
      if (opStatsOn) ob[std::string("mir.") + std::to_string(unsigned(in.op))] += here() - was;
    }
  }
  if (opStatsOn) {
    std::lock_guard<std::mutex> l(opBytesMu);
    for (auto &[k, v] : ob) opBytesAll[k] += v;
  }
  for (const Fixup &x : fixups) {
    int64_t off = int64_t(blockStart[x.block]) - int64_t(x.at);
    if (x.kind != Fixup::B && (off >= (int64_t(1) << 20) || off < -(int64_t(1) << 20)))
      unsupported("conditional branch out of range");
    uint32_t word = x.kind == Fixup::B       ? a64::b(int32_t(off))
                    : x.kind == Fixup::BCond ? bcond(x.cond, int32_t(off))
                    : x.kind == Fixup::Cbnz  ? cbnz(x.reg, int32_t(off))
                                             : cbz(x.reg, int32_t(off));
    std::memcpy(&w.bytes(Sect::Text)[x.at], &word, 4);
  }
  auto lk = fn.getLinkage();
  std::string sym = machoName(fn.getSymName());
  w.define(sym, Sect::Text, fnStart, !isLocalLinkage(lk), isWeakLinkage(lk), isHidden(fn));
  mg.lines.push_back({w.symbol(sym), here() - fnStart, std::move(rows), {}, {}});
}

} // namespace

// Compiles fn through MIR; false when MIR does not cover it (nothing was
// emitted then).
bool mirArm64(ModuleGen &mg, LL::LLVMFuncOp fn) {
  // MARMOT_TIER0_MIR_BISECT=LO-HI: only functions whose name hashes (FNV-1a,
  // mod 4096) into [LO, HI) take MIR; for finding a miscompiled function.
  static const std::pair<unsigned, unsigned> range = [] {
    unsigned lo = 0, hi = 4096;
    if (const char *e = getenv("MARMOT_TIER0_MIR_BISECT")) sscanf(e, "%u-%u", &lo, &hi);
    return std::make_pair(lo, hi);
  }();
  if (range.first != 0 || range.second != 4096) {
    uint32_t h = 2166136261u;
    for (char ch : fn.getSymName()) h = (h ^ uint8_t(ch)) * 16777619u;
    if (h % 4096 < range.first || h % 4096 >= range.second) return false;
    if (getenv("TIER0_MIR_WHY")) fprintf(stderr, "mir: %s\n", fn.getSymName().str().c_str());
  }
  auto cp = mg.w.checkpoint();
  try {
    Emitter(mg, fn).run();
    return true;
  } catch (const Unsupported &u) {
    mg.w.rollback(cp);
    static const bool why = getenv("TIER0_MIR_WHY") != nullptr;
    if (why) fprintf(stderr, "mir fallback %s: %s\n", fn.getSymName().str().c_str(), u.why.c_str());
    return false;
  }
}

} // namespace tier0
} // namespace marmot

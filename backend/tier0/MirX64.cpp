// MirX64.cpp: the x86-64 emitter for MIR (SysV, as LLVM lowers it, ELF).
// Values live where MirAlloc put them: a register for the whole interval, a
// spill slot, or made again at each use. Floats live in xmm registers.
//
// Frame, from rsp up: outgoing stack arguments, the callee saved registers
// used, the spill slots, an 8-byte temporary (x87 results), the saved result
// pointer of a function returning in memory, the frame objects; then rbp and
// the return address.
#include "Abi.h"
#include "MirEmit.h"
#include "X64.h"

#include <cstring>

namespace marmot {
namespace tier0 {

bool mirX64(ModuleGen &mg, LL::LLVMFuncOp fn);

namespace {

using namespace mir;
namespace X = marmot::tier0::x64;

// Scratch: rax, rcx (shift counts), rdx (division) inside an operation;
// r11 for moves, r10 to break move cycles; xmm13 inside an operation,
// xmm14 for moves, xmm15 for cycles. Allocated: rsi, rdi, r8, r9 for values
// no call meets, rbx and r12-r15 otherwise; xmm0-xmm12 (none survives a
// call in SysV).
constexpr unsigned S0 = X::RAX, S1 = X::RCX, S2 = X::RDX, MV = X::R11, CY = X::R10;
constexpr unsigned F0 = 13, FM = 14, FC = 15;

const TargetRegs &regs() {
  static const TargetRegs t = [] {
    TargetRegs r;
    r.gpr.caller = {X::RSI, X::RDI, X::R8, X::R9};
    r.gpr.callee = {X::RBX, X::R12, X::R13, X::R14, X::R15};
    r.fpr.caller = {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12};
    r.fmaIsCall = true;
    // Two-address results in their first operand's register (MARMOT_TIER0_TIE=0 turns it off).
    const char *tie = getenv("MARMOT_TIER0_TIE");
    r.tieFirst = !(tie && *tie == '0');
    return r;
  }();
  return t;
}

static X::CC icmpCC(LL::ICmpPredicate p) {
  using P = LL::ICmpPredicate;
  switch (p) {
  case P::eq: return X::E;
  case P::ne: return X::NE;
  case P::slt: return X::L;
  case P::sle: return X::LE;
  case P::sgt: return X::G;
  case P::sge: return X::GE;
  case P::ult: return X::B;
  case P::ule: return X::BE;
  case P::ugt: return X::A;
  case P::uge: return X::AE;
  }
  return X::E;
}

class Emitter : public MoveOps {
public:
  Emitter(ModuleGen &mg, LL::LLVMFuncOp fn) : mg(mg), w(mg.w), fn(fn), a(w.bytes(Sect::Text)) {}
  void run();

private:
  ModuleGen &mg;
  MachOWriter &w;
  LL::LLVMFuncOp fn;
  X::Asm a;
  Func f;
  Alloc al;

  uint64_t frameSize = 0, saveArea = 0, slotBase = 0, tempOff = 0, sretSave = 0;
  std::vector<uint64_t> objOff;
  bool sret = false;
  std::vector<unsigned> saved;

  uint64_t fnStart = 0;
  std::vector<uint64_t> blockStart;
  std::vector<char> emitted;
  uint32_t nextBlock = 0;
  struct Jump {
    uint64_t at;
    bool cond;
    X::CC cc;
    int block;  // or -1 for a label
    int label;
  };
  std::vector<Jump> jumps;
  std::vector<int64_t> labels;
  int epiLabel = -1;

  int32_t d32(int64_t off) {
    if (off < INT32_MIN || off > INT32_MAX) unsupported("mir: displacement out of range");
    return int32_t(off);
  }
  int newLabel() {
    labels.push_back(-1);
    return int(labels.size() - 1);
  }
  void bind(int l) { labels[size_t(l)] = int64_t(a.here()); }
  void jumpTo(bool cond, X::CC cc, int block, int label) {
    jumps.push_back({a.here(), cond, cc, block, label});
    if (cond)
      a.jcc(cc);
    else
      a.jmp();
  }
  void branchTo(uint32_t b) { jumpTo(false, X::O, int(b), -1); }
  void jccTo(X::CC cc, uint32_t b) { jumpTo(true, cc, int(b), -1); }
  void patch32(size_t field, int64_t target) {
    int32_t rel = int32_t(target - int64_t(field + 4));
    std::memcpy(&w.bytes(Sect::Text)[field], &rel, 4);
  }
  void relax(size_t relocFrom, std::vector<ModuleGen::Row> &rows);

  uint64_t slotOff(uint32_t s) const { return slotBase + 8 * uint64_t(s); }
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
  // The base register and displacement of [v + imm]: a frame object's
  // address folds into rsp + offset.
  std::pair<unsigned, int64_t> addrOf(VId v, int64_t imm, unsigned scratch) {
    if (al.loc[v].k == Loc::Remat) {
      const Inst &d = defOf(v);
      if (d.op == Op::Frame) return {X::RSP, int64_t(objOff[d.aux]) + d.imm + imm};
    }
    return {gp(v, scratch), imm};
  }
  void remat(unsigned r, VId v) {
    const Inst &d = defOf(v);
    if (d.op == Op::Const)
      a.movImm(r, uint64_t(d.imm));
    else
      a.lea(r, X::RSP, d32(int64_t(objOff[d.aux]) + d.imm));
  }
  unsigned gp(VId v, unsigned scratch) {
    const Loc &l = al.loc[v];
    switch (l.k) {
    case Loc::Reg: return l.reg;
    case Loc::Slot: a.load(scratch, X::RSP, d32(int64_t(slotOff(l.slot))), 8); return scratch;
    case Loc::Remat: remat(scratch, v); return scratch;
    default: unsupported("mir: value without a place");
    }
  }
  unsigned fr(VId v, unsigned scratch) {
    const Loc &l = al.loc[v];
    switch (l.k) {
    case Loc::Reg: return l.reg;
    case Loc::Slot: a.loadF(scratch, X::RSP, d32(int64_t(slotOff(l.slot))), dbl(v)); return scratch;
    case Loc::Remat: {
      uint64_t bits;
      konst(v, bits);
      a.movImm(MV, bits);
      a.movqToX(scratch, MV);
      return scratch;
    }
    default: unsupported("mir: value without a place");
    }
  }
  void signExtend(unsigned r, unsigned wdt) {
    switch (wdt) {
    case 64: break;
    case 32: a.movsx32(r, r); break;
    case 16: a.movsx16(r, r); break;
    case 8: a.movsx8(r, r); break;
    default:
      a.shiftImm(4, r, uint8_t(64 - wdt));
      a.shiftImm(7, r, uint8_t(64 - wdt));
    }
  }
  void norm(unsigned r, unsigned wdt) {
    switch (wdt) {
    case 64: break;
    case 32: a.mov32(r, r); break;
    case 16: a.movzx16(r, r); break;
    case 8: a.movzx8(r, r); break;
    case 1: a.andImm(r, 1); break;
    default:
      a.shiftImm(4, r, uint8_t(64 - wdt));
      a.shiftImm(5, r, uint8_t(64 - wdt));
    }
  }
  // v sign extended from wdt bits, in scratch unless it already is.
  unsigned gpSigned(VId v, unsigned wdt, unsigned scratch) {
    unsigned r = gp(v, scratch);
    if (wdt >= 64) return r;
    if (r != scratch) a.mov(scratch, r);
    signExtend(scratch, wdt);
    return scratch;
  }
  unsigned dstG(VId v, unsigned scratch) {
    if (v != None && al.loc[v].k == Loc::Reg) return al.loc[v].reg;
    return scratch;
  }
  void finG(VId v, unsigned r) {
    if (v == None) return;
    const Loc &l = al.loc[v];
    if (l.k == Loc::Reg) {
      if (l.reg != r) a.mov(l.reg, r);
    } else if (l.k == Loc::Slot) {
      a.store(r, X::RSP, d32(int64_t(slotOff(l.slot))), 8);
    }
  }
  void finF(VId v, unsigned x) {
    if (v == None) return;
    const Loc &l = al.loc[v];
    if (l.k == Loc::Reg) {
      if (l.reg != x) a.movaps(l.reg, x);
    } else if (l.k == Loc::Slot) {
      a.storeF(x, X::RSP, d32(int64_t(slotOff(l.slot))), dbl(v));
    }
  }
  Place place(VId v) const {
    Place p;
    if (v == None) return p;
    const Loc &l = al.loc[v];
    p.fp = f.vals[v].cls != Cls::Int;
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

  void move(const Place &dst, const Place &src) override;
  unsigned cycleScratch(bool fp) override { return fp ? FC : CY; }

  void layout();
  void hints();
  bool sretFn() {
    Type rt = fn.getFunctionType().getReturnType();
    return !isa<LL::LLVMVoidType>(rt) && returnsInMemoryX64(rt);
  }
  void prologue();
  void epilogue();
  void emit(uint32_t i);
  void binop(const Inst &in);
  void cmpFlags(const Inst &c);
  void fminmax(const Inst &in, bool max, bool num);
  void call(const Inst &in);
  void ret(const Inst &in);
  void edgeMoves(uint32_t e);
  void callSym(const std::string &name) {
    size_t at = a.call();
    w.reloc(Sect::Text, at, Reloc::Call32, w.symbol(name));
  }
  void copyMem(unsigned db, int64_t doff, unsigned sb, int64_t soff, uint64_t n);
  void zeroMem(unsigned base, int64_t off, uint64_t n);
};

void Emitter::move(const Place &dst, const Place &src) {
  auto load = [&](unsigned r, bool fp) {
    switch (src.k) {
    case Place::Reg:
      if (src.fp == fp) {
        if (src.reg != r) fp ? a.movaps(r, src.reg) : a.mov(r, src.reg);
      } else if (fp) {
        a.movqToX(r, src.reg);
      } else {
        a.movqFromX(r, src.reg);
      }
      break;
    case Place::Slot:
      if (fp)
        a.loadF(r, X::RSP, d32(int64_t(slotOff(src.slot))), true);
      else
        a.load(r, X::RSP, d32(int64_t(slotOff(src.slot))), 8);
      break;
    case Place::Mem:
      if (fp)
        a.loadF(r, src.base, d32(src.off), src.size == 8);
      else
        a.load(r, src.base, d32(src.off), src.size);
      break;
    case Place::Remat: {
      const Inst &d = f.insts[src.inst];
      if (fp) {
        a.movImm(MV, uint64_t(d.imm));
        a.movqToX(r, MV);
      } else if (d.op == Op::Const) {
        a.movImm(r, uint64_t(d.imm));
      } else {
        a.lea(r, X::RSP, d32(int64_t(objOff[d.aux]) + d.imm));
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
    if (src.k == Place::Reg && src.fp == fp) {
      r = src.reg;
    } else {
      r = fp ? FM : MV;
      load(r, fp);
    }
    if (dst.k == Place::Slot) {
      if (fp)
        a.storeF(r, X::RSP, d32(int64_t(slotOff(dst.slot))), true);
      else
        a.store(r, X::RSP, d32(int64_t(slotOff(dst.slot))), 8);
    } else if (fp) {
      a.storeF(r, dst.base, d32(dst.off), dst.size == 8);
    } else {
      a.store(r, dst.base, d32(dst.off), dst.size);
    }
    return;
  }
  default: return;
  }
}

void Emitter::layout() {
  uint64_t out = 0;
  for (const CallRec &c : f.calls) {
    uint64_t n = 0;
    auto op = cast<LL::CallOp>(c.op);
    bool mem = op.getNumResults() == 1 && returnsInMemoryX64(op.getResult().getType());
    assignArgsX64(op.getArgOperands().getTypes(), n, mem);
    out = std::max(out, n);
  }
  frameSize = (out + 15) & ~uint64_t(15);
  for (unsigned r : {X::RBX, X::R12, X::R13, X::R14, X::R15})
    if (al.calleeGpr >> r & 1) saved.push_back(r);
  saveArea = frameSize;
  frameSize += 8 * saved.size();
  slotBase = frameSize;
  frameSize += 8 * uint64_t(al.slots);
  tempOff = frameSize;
  frameSize += 8;
  auto rt = fn.getFunctionType().getReturnType();
  if (!isa<LL::LLVMVoidType>(rt) && returnsInMemoryX64(rt)) {
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
  if (frameSize >= (uint64_t(1) << 31)) unsupported("frame larger than 2 GiB");
}

void Emitter::prologue() {
  a.push(X::RBP);
  a.mov(X::RBP, X::RSP);
  if (frameSize) a.subImm(X::RSP, int32_t(frameSize));
  uint64_t at = saveArea;
  for (unsigned r : saved) {
    a.store(r, X::RSP, d32(int64_t(at)), 8);
    at += 8;
  }
  if (sret) a.store(X::RDI, X::RSP, d32(int64_t(sretSave)), 8);
  uint64_t stackSize = 0;
  std::vector<ArgLoc> locs = assignArgsX64(TypeRange(fn.getFunctionType().getParams()), stackSize, sret);
  if (locs.size() != f.params.size()) unsupported("mir: parameter pieces");
  std::vector<Move> moves;
  for (size_t i = 0; i < locs.size(); i++) {
    const ArgLoc &l = locs[i];
    VId v = f.params[i];
    if (al.loc[v].k == Loc::None) continue;
    Place src;
    src.fp = l.fp;
    if (l.reg >= 0) {
      src.k = Place::Reg;
      src.reg = uint8_t(l.reg);
    } else {
      src.k = Place::Mem;
      src.base = X::RBP;
      src.off = int64_t(16 + l.stack);
      src.size = uint8_t(sizeOf(l.t));
    }
    moves.push_back({place(v), src});
  }
  parallelMove(moves, *this);
  for (size_t i = 0; i < locs.size(); i++) {
    VId v = f.params[i];
    if (locs[i].fp || al.loc[v].k == Loc::None || f.vals[v].width >= 64) continue;
    unsigned r = gp(v, S0);
    norm(r, f.vals[v].width);
    finG(v, r);
  }
}

void Emitter::epilogue() {
  if (epiLabel >= 0) {
    jumpTo(false, X::O, -1, epiLabel);
    return;
  }
  if (!saved.empty()) {
    epiLabel = newLabel();
    bind(epiLabel);
  }
  uint64_t at = saveArea;
  for (unsigned r : saved) {
    a.load(r, X::RSP, d32(int64_t(at)), 8);
    at += 8;
  }
  if (frameSize)
    a.leave();
  else
    a.pop(X::RBP);
  a.ret();
}

void Emitter::copyMem(unsigned db, int64_t doff, unsigned sb, int64_t soff, uint64_t n) {
  uint64_t i = 0;
  for (; i + 16 <= n; i += 16) {
    a.movupsLoad(F0, sb, d32(soff + int64_t(i)));
    a.movupsStore(F0, db, d32(doff + int64_t(i)));
  }
  for (unsigned sz = 8; sz >= 1; sz /= 2)
    for (; i + sz <= n; i += sz) {
      a.load(S0, sb, d32(soff + int64_t(i)), sz);
      a.store(S0, db, d32(doff + int64_t(i)), sz);
    }
}

void Emitter::zeroMem(unsigned base, int64_t off, uint64_t n) {
  uint64_t i = 0;
  if (n >= 16) {
    a.xor_(S0, S0);  // flags are dead between operations
    for (; i + 8 <= n; i += 8) a.store(S0, base, d32(off + int64_t(i)), 8);
  }
  for (unsigned sz = 8; sz >= 1; sz /= 2)
    for (; i + sz <= n; i += sz) a.storeImm(base, d32(off + int64_t(i)), sz, 0);
}

void Emitter::cmpFlags(const Inst &c) {
  bool sgn = isSignedPred(LL::ICmpPredicate(c.aux));
  unsigned l = sgn ? gpSigned(c.a, c.w, S0) : gp(c.a, S0);
  uint64_t bits;
  if (konst(c.b, bits)) {
    int64_t v = sgn && c.w < 64 ? int64_t(bits << (64 - c.w)) >> (64 - c.w) : int64_t(bits);
    if (sgn ? v == int64_t(int32_t(v)) : bits < (uint64_t(1) << 31)) {
      if (v == 0 && !sgn)
        a.test(l, l);
      else
        a.cmpImm(l, int32_t(v));
      return;
    }
  }
  unsigned r = sgn ? gpSigned(c.b, c.w, S1) : gp(c.b, S1);
  a.cmp(l, r);
}

// MARMOT_TIER0_ALU32=1: a 32-bit add, sub, and, or, xor or mul uses the
// 32-bit instruction, which zero extends its result, instead of the 64-bit
// one and a mov to zero extend (operands are canonical, so the low 32 bits
// agree).
static const bool alu32On = [] {
  const char *e = getenv("MARMOT_TIER0_ALU32");
  return e && *e == '1';
}();

void Emitter::binop(const Inst &in) {
  unsigned d = dstG(in.dst, S0);
  uint64_t k;
  bool ck = konst(in.b, k);
  switch (in.op) {
  case Op::Add: case Op::Sub: case Op::And: case Op::Or: case Op::Xor: case Op::Mul: {
    const bool w = !(alu32On && in.w == 32);
    unsigned x = gp(in.a, S0);
    if (ck && int64_t(k) == int64_t(int32_t(k)) && in.op != Op::Mul) {
      if (d != x) a.mov(d, x);
      unsigned kind = in.op == Op::Add ? 0 : in.op == Op::Or ? 1 : in.op == Op::And ? 4 : in.op == Op::Sub ? 5 : 6;
      a.aluImm(kind, d, int32_t(k), w);
    } else {
      unsigned y = gp(in.b, S1);
      if (d != x) a.mov(d, x);
      switch (in.op) {
      case Op::Add: a.add(d, y, w); break;
      case Op::Sub: a.sub(d, y, w); break;
      case Op::And: a.and_(d, y, w); break;
      case Op::Or: a.or_(d, y, w); break;
      case Op::Xor: a.xor_(d, y, w); break;
      default: a.imul(d, y, w); break;
      }
    }
    if (!w) {  // already zero extended from 32 bits
      finG(in.dst, d);
      return;
    }
    break;
  }
  case Op::Shl: case Op::LShr: case Op::AShr: {
    unsigned kind = in.op == Op::Shl ? 4 : in.op == Op::LShr ? 5 : 7;
    if (ck) {
      unsigned x = in.op == Op::AShr ? gpSigned(in.a, in.w, S0) : gp(in.a, S0);
      if (d != x) a.mov(d, x);
      if (k & 63) a.shiftImm(kind, d, uint8_t(k & 63));
    } else {
      unsigned y = gp(in.b, S1);
      if (y != S1) a.mov(S1, y);
      unsigned x = in.op == Op::AShr ? gpSigned(in.a, in.w, S0) : gp(in.a, S0);
      if (d != x) a.mov(d, x);
      a.shiftCl(kind, d);
    }
    break;
  }
  case Op::UDiv: case Op::URem: case Op::SDiv: case Op::SRem: {
    bool sgn = in.op == Op::SDiv || in.op == Op::SRem;
    unsigned y = sgn ? gpSigned(in.b, in.w, S1) : gp(in.b, S1);
    unsigned x = sgn ? gpSigned(in.a, in.w, S0) : gp(in.a, S0);
    if (x != S0) a.mov(S0, x);
    if (sgn) {
      a.cqo();
      a.idiv(y);
    } else {
      a.xor_(S2, S2);
      a.div(y);
    }
    unsigned r = in.op == Op::URem || in.op == Op::SRem ? S2 : S0;
    if (d != r) a.mov(d, r);
    break;
  }
  case Op::UMulH: {
    unsigned y = gp(in.b, S1);
    unsigned x = gp(in.a, S0);
    if (x != S0) a.mov(S0, x);
    a.mulRdx(y);
    if (d != S2) a.mov(d, S2);
    break;
  }
  default: unsupported("mir: binop");
  }
  norm(d, in.w);
  finG(in.dst, d);
}

// llvm.maximum / minimum (NaN propagates, -0 < +0) and maxnum / minnum (a
// NaN operand gives the other one), through the bits in rax.
void Emitter::fminmax(const Inst &in, bool max, bool num) {
  bool dx = dbl(in.dst);
  unsigned x = fr(in.a, F0);
  if (x != F0) a.movaps(F0, x);
  unsigned y = fr(in.b, FM);
  if (y != FM) a.movaps(FM, y);
  std::vector<size_t> toEnd;
  if (num) {
    a.ucomi(F0, F0, dx);
    size_t j = a.jcc(X::NP);
    a.movqFromX(S0, FM);
    toEnd.push_back(a.jmp());
    patch32(j, int64_t(a.here()));
    a.ucomi(FM, FM, dx);
    j = a.jcc(X::NP);
    a.movqFromX(S0, F0);
    toEnd.push_back(a.jmp());
    patch32(j, int64_t(a.here()));
  }
  a.ucomi(F0, FM, dx);
  size_t unord = a.jcc(X::P);
  size_t ne = a.jcc(X::NE);
  a.movqFromX(S0, F0);
  a.movqFromX(S1, FM);
  if (max)
    a.and_(S0, S1);
  else
    a.or_(S0, S1);
  toEnd.push_back(a.jmp());
  patch32(ne, int64_t(a.here()));
  if (max)
    a.maxsd(F0, FM, dx);
  else
    a.minsd(F0, FM, dx);
  a.movqFromX(S0, F0);
  toEnd.push_back(a.jmp());
  patch32(unord, int64_t(a.here()));
  a.addsd(F0, FM, dx);
  a.movqFromX(S0, F0);
  for (size_t j : toEnd) patch32(j, int64_t(a.here()));
  if (!dx) a.mov32(S0, S0);
  unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
  a.movqToX(d, S0);
  finF(in.dst, d);
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
  bool mem = c.sretObj >= 0;
  uint64_t stackSize = 0;
  std::vector<ArgLoc> locs = assignArgsX64(args.getTypes(), stackSize, mem);
  if (locs.size() != c.args.size()) unsupported("mir: call pieces");
  std::vector<Move> moves;
  std::vector<std::pair<unsigned, unsigned>> sext;
  for (size_t i = 0; i < locs.size(); i++) {
    const ArgLoc &l = locs[i];
    Place d;
    d.fp = l.fp;
    if (l.reg >= 0) {
      d.k = Place::Reg;
      d.reg = uint8_t(l.reg);
    } else {
      d.k = Place::Mem;
      d.base = X::RSP;
      d.off = int64_t(l.stack);
      d.size = 8;
    }
    if (!l.fp && intWidth(l.t) < 64 && mg.signExtArg(op, l.arg)) {
      if (l.reg < 0) unsupported("mir: sign extended stack argument");
      sext.push_back({unsigned(l.reg), intWidth(l.t)});
    }
    moves.push_back({d, place(c.args[i])});
  }
  if (c.indirect) moves.push_back({Place::reg_(S0, false), place(c.callee)});
  parallelMove(moves, *this);
  for (auto [r, wd] : sext) signExtend(r, wd);
  if (mem) a.lea(X::RDI, X::RSP, d32(int64_t(objOff[size_t(c.sretObj)])));
  if (c.indirect)
    a.callReg(S0);
  else
    callSym(symName(f.syms[c.sym]));
  if (c.results.empty()) return;
  std::vector<std::pair<uint64_t, Type>> parts;
  flatten(op.getResult().getType(), 0, parts);
  if (parts.size() != c.results.size()) unsupported("mir: result pieces");
  // Results: rax, rdx, rcx; xmm0, xmm1, then st0, st1 (popped through the
  // temporary into xmm13 and xmm14: not result registers, not allocated).
  unsigned ngp = 0, nfp = 0;
  moves.clear();
  std::vector<std::pair<size_t, unsigned>> x87;  // (piece, xmm it is popped into)
  for (size_t i = 0; i < parts.size(); i++) {
    Type t = parts[i].second;
    VId v = c.results[i];
    if (isFloat(t)) {
      unsigned k = nfp++;
      if (k >= 4) unsupported("result returned in memory");
      if (k >= 2) {
        x87.push_back({i, k == 2 ? F0 : FM});
        if (al.loc[v].k != Loc::None) moves.push_back({place(v), Place::reg_(k == 2 ? F0 : FM, true)});
        continue;
      }
      if (al.loc[v].k != Loc::None) moves.push_back({place(v), Place::reg_(k, true)});
    } else {
      if (ngp >= 3) unsupported("result returned in memory");
      unsigned r = retGpr[ngp++];
      if (intWidth(t) < 64) norm(r, intWidth(t));
      if (al.loc[v].k != Loc::None) moves.push_back({place(v), Place::reg_(r, false)});
    }
  }
  for (auto [i, x] : x87) {  // st0 first
    bool dx = parts[i].second.isF64();
    a.fstp(X::RSP, d32(int64_t(tempOff)), dx);
    a.loadF(x, X::RSP, d32(int64_t(tempOff)), dx);
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
      a.load(MV, X::RSP, d32(int64_t(sretSave)), 8);
      for (size_t i = 0; i < parts.size(); i++) {
        auto [off, t] = parts[i];
        VId v = r.pieces[i];
        if (isFloat(t))
          a.storeF(fr(v, F0), MV, d32(int64_t(off)), t.isF64());
        else
          a.store(gp(v, S0), MV, d32(int64_t(off)), unsigned(sizeOf(t)));
      }
      a.mov(X::RAX, MV);
    } else {
      // Third and fourth floats on the x87 stack, the fourth pushed first.
      std::vector<size_t> fps;
      for (size_t i = 0; i < parts.size(); i++)
        if (isFloat(parts[i].second)) fps.push_back(i);
      if (fps.size() > 4) unsupported("result returned in memory");
      for (size_t k = fps.size(); k-- > 2;) {
        size_t i = fps[k];
        bool dx = parts[i].second.isF64();
        a.storeF(fr(r.pieces[i], F0), X::RSP, d32(int64_t(tempOff)), dx);
        a.fld(X::RSP, d32(int64_t(tempOff)), dx);
      }
      std::vector<Move> moves;
      std::vector<std::pair<unsigned, unsigned>> sext;
      unsigned ngp = 0, nfp = 0;
      bool signExt = ModuleGen::signExtResult(fn);
      for (size_t i = 0; i < parts.size(); i++) {
        Type t = parts[i].second;
        if (isFloat(t)) {
          unsigned k = nfp++;
          if (k < 2) moves.push_back({Place::reg_(k, true), place(r.pieces[i])});
          continue;
        }
        if (ngp >= 3) unsupported("result returned in memory");
        unsigned rg = retGpr[ngp++];
        moves.push_back({Place::reg_(rg, false), place(r.pieces[i])});
        if (signExt && intWidth(t) < 64) sext.push_back({rg, intWidth(t)});
      }
      parallelMove(moves, *this);
      for (auto [rg, wd] : sext) signExtend(rg, wd);
    }
  }
  epilogue();
}

void Emitter::emit(uint32_t i) {
  const Inst &in = f.insts[i];
  switch (in.op) {
  case Op::Const:
  case Op::Frame:
    return;
  case Op::Addr: {
    unsigned d = dstG(in.dst, S0);
    const std::string &name = f.syms[in.sym];
    unsigned s = w.symbol(symName(name));
    if (mg.isDefinedData(name))
      w.reloc(Sect::Text, a.leaRip(d), Reloc::Pc32, s);
    else
      w.reloc(Sect::Text, a.loadRip(d), Reloc::GotPc32, s);
    if (in.imm) a.addImm(d, d32(in.imm));
    finG(in.dst, d);
    return;
  }
  case Op::Copy: {
    if (isFp(in.dst)) {
      unsigned x = fr(in.a, F0);
      finF(in.dst, x);
    } else {
      finG(in.dst, gp(in.a, S0));
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
    a.setcc(icmpCC(LL::ICmpPredicate(in.aux)), d);
    a.movzx8(d, d);
    finG(in.dst, d);
    return;
  }
  case Op::Select: {
    unsigned c = gp(in.a, S1);
    if (isFp(in.dst)) {
      unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
      unsigned x = fr(in.b, FM);
      if (x != FM) a.movaps(FM, x);
      unsigned y = fr(in.c, F0);
      a.test(c, c);
      size_t jz = a.jcc(X::E);
      a.movaps(d, FM);
      size_t j = a.jmp();
      patch32(jz, int64_t(a.here()));
      if (d != y) a.movaps(d, y);
      patch32(j, int64_t(a.here()));
      finF(in.dst, d);
      return;
    }
    unsigned t = gp(in.b, S2);
    unsigned fv = gp(in.c, S0);
    unsigned d = dstG(in.dst, S0);
    if (d != fv) a.mov(d, fv);
    a.test(c, c);
    a.cmov(X::NE, d, t);
    finG(in.dst, d);
    return;
  }
  case Op::SExt: {
    unsigned x = gp(in.a, S0), d = dstG(in.dst, S0);
    if (d != x) a.mov(d, x);
    signExtend(d, in.aux);
    norm(d, in.w);
    finG(in.dst, d);
    return;
  }
  case Op::Trunc: {
    unsigned x = gp(in.a, S0), d = dstG(in.dst, S0);
    if (d != x) a.mov(d, x);
    norm(d, in.w);
    finG(in.dst, d);
    return;
  }
  case Op::FAdd: case Op::FSub: case Op::FMul: case Op::FDiv: {
    bool dx = dbl(in.dst);
    unsigned y = fr(in.b, FM);
    unsigned x = fr(in.a, F0);
    unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
    if (d == y) {  // d already holds b: compute in F0
      if (x != F0) a.movaps(F0, x);
      d = F0;
    } else if (d != x) {
      a.movaps(d, x);
    }
    switch (in.op) {
    case Op::FAdd: a.addsd(d, y, dx); break;
    case Op::FSub: a.subsd(d, y, dx); break;
    case Op::FMul: a.mulsd(d, y, dx); break;
    default: a.divsd(d, y, dx); break;
    }
    finF(in.dst, d);
    return;
  }
  case Op::FMax: fminmax(in, true, false); return;
  case Op::FMin: fminmax(in, false, false); return;
  case Op::FMaxNum: fminmax(in, true, true); return;
  case Op::FMinNum: fminmax(in, false, true); return;
  case Op::FNeg: {
    unsigned x = fr(in.a, F0);
    a.movqFromX(S0, x);
    a.movImm(S1, dbl(in.dst) ? uint64_t(1) << 63 : uint64_t(1) << 31);
    a.xor_(S0, S1);
    unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
    a.movqToX(d, S0);
    finF(in.dst, d);
    return;
  }
  case Op::FMA: {
    // No FMA instruction at x86-64-v2: the C library's fma.
    std::vector<Move> moves{{Place::reg_(0, true), place(in.a)},
                            {Place::reg_(1, true), place(in.b)},
                            {Place::reg_(2, true), place(in.c)}};
    parallelMove(moves, *this);
    callSym(symName(dbl(in.dst) ? "fma" : "fmaf"));
    finF(in.dst, 0);
    return;
  }
  case Op::FMulAdd: {
    bool dx = dbl(in.dst);
    unsigned x = fr(in.a, F0);
    if (x != F0) a.movaps(F0, x);
    a.mulsd(F0, fr(in.b, FM), dx);
    a.addsd(F0, fr(in.c, FM), dx);
    finF(in.dst, F0);
    return;
  }
  case Op::FCmp: {
    bool dx = in.w == 64;
    unsigned x = fr(in.a, F0);
    if (x != F0) a.movaps(F0, x);
    unsigned y = fr(in.b, FM);
    using P = LL::FCmpPredicate;
    auto one = [&](bool swap, X::CC k) {
      swap ? a.ucomi(y, F0, dx) : a.ucomi(F0, y, dx);
      a.setcc(k, S0);
      a.movzx8(S0, S0);
    };
    auto two = [&](X::CC k1, X::CC k2, bool both) {
      a.ucomi(F0, y, dx);
      a.setcc(k1, S0);
      a.setcc(k2, S1);
      a.movzx8(S0, S0);
      a.movzx8(S1, S1);
      both ? a.and_(S0, S1) : a.or_(S0, S1);
    };
    switch (P(in.aux)) {
    case P::_false: a.movImm(S0, 0); break;
    case P::_true: a.movImm(S0, 1); break;
    case P::oeq: two(X::E, X::NP, true); break;
    case P::ogt: one(false, X::A); break;
    case P::oge: one(false, X::AE); break;
    case P::olt: one(true, X::A); break;
    case P::ole: one(true, X::AE); break;
    case P::one: one(false, X::NE); break;
    case P::ord: one(false, X::NP); break;
    case P::ueq: one(false, X::E); break;
    case P::ugt: one(true, X::B); break;
    case P::uge: one(true, X::BE); break;
    case P::ult: one(false, X::B); break;
    case P::ule: one(false, X::BE); break;
    case P::une: two(X::NE, X::P, false); break;
    case P::uno: one(false, X::P); break;
    }
    finG(in.dst, S0);
    return;
  }
  case Op::SIToFP: {
    unsigned x = gpSigned(in.a, in.w, S0);
    unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
    a.cvtsi2f(d, x, dbl(in.dst));
    finF(in.dst, d);
    return;
  }
  case Op::UIToFP: {
    bool dx = dbl(in.dst);
    unsigned x = gp(in.a, S0);
    if (x != S0) a.mov(S0, x);
    unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
    // Below 2^63 a signed conversion; above, halve (keeping the low bit for
    // rounding), convert and double.
    a.test(S0, S0);
    size_t neg = a.jcc(X::S);
    a.cvtsi2f(d, S0, dx);
    size_t j = a.jmp();
    patch32(neg, int64_t(a.here()));
    a.mov(S1, S0);
    a.shiftImm(5, S1, 1);
    a.andImm(S0, 1);
    a.or_(S1, S0);
    a.cvtsi2f(d, S1, dx);
    a.addsd(d, d, dx);
    patch32(j, int64_t(a.here()));
    finF(in.dst, d);
    return;
  }
  case Op::FPToSI: {
    unsigned x = fr(in.a, F0), d = dstG(in.dst, S0);
    a.cvttf2si(d, x, in.w == 64);
    norm(d, f.vals[in.dst].width);
    finG(in.dst, d);
    return;
  }
  case Op::FPToUI: {
    bool dx = in.w == 64;
    unsigned x = fr(in.a, F0);
    if (x != F0) a.movaps(F0, x);
    a.movImm(S1, dx ? 0x43E0000000000000ull : 0x5F000000ull);
    a.movqToX(FM, S1);
    a.ucomi(F0, FM, dx);
    size_t big = a.jcc(X::AE);
    a.cvttf2si(S0, F0, dx);
    size_t j = a.jmp();
    patch32(big, int64_t(a.here()));
    a.subsd(F0, FM, dx);
    a.cvttf2si(S0, F0, dx);
    a.movImm(S1, uint64_t(1) << 63);
    a.xor_(S0, S1);
    patch32(j, int64_t(a.here()));
    norm(S0, f.vals[in.dst].width);
    finG(in.dst, S0);
    return;
  }
  case Op::FPToSISat:
  case Op::FPToUISat: {
    bool dx = in.w == 64;
    bool sgn = in.op == Op::FPToSISat;
    unsigned x = fr(in.a, F0);
    if (x != F0) a.movaps(F0, x);
    std::vector<size_t> toEnd;
    a.xor_(S0, S0);
    a.ucomi(F0, F0, dx);
    toEnd.push_back(a.jcc(X::P));  // NaN: 0
    a.movImm(S1, sgn ? (dx ? 0x43E0000000000000ull : 0x5F000000ull) : (dx ? 0x43F0000000000000ull : 0x5F800000ull));
    a.movqToX(FM, S1);
    a.movImm(S0, sgn ? uint64_t(INT64_MAX) : ~uint64_t(0));
    a.ucomi(F0, FM, dx);
    toEnd.push_back(a.jcc(X::AE));
    if (sgn) {
      a.cvttf2si(S0, F0, dx);  // below -2^63 gives INT64_MIN, as saturation wants
    } else {
      a.xor_(S0, S0);
      a.xor_(S1, S1);
      a.movqToX(FM, S1);
      a.ucomi(F0, FM, dx);
      toEnd.push_back(a.jcc(X::BE));  // at or below zero: 0
      a.movImm(S1, dx ? 0x43E0000000000000ull : 0x5F000000ull);
      a.movqToX(FM, S1);
      a.ucomi(F0, FM, dx);
      size_t big = a.jcc(X::AE);
      a.cvttf2si(S0, F0, dx);
      toEnd.push_back(a.jmp());
      patch32(big, int64_t(a.here()));
      a.subsd(F0, FM, dx);
      a.cvttf2si(S0, F0, dx);
      a.movImm(S1, uint64_t(1) << 63);
      a.xor_(S0, S1);
    }
    for (size_t j : toEnd) patch32(j, int64_t(a.here()));
    finG(in.dst, S0);
    return;
  }
  case Op::FPExt:
  case Op::FPTrunc: {
    unsigned x = fr(in.a, F0);
    unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
    if (in.op == Op::FPExt)
      a.cvtss2sd(d, x);
    else
      a.cvtsd2ss(d, x);
    finF(in.dst, d);
    return;
  }
  case Op::Bits: {
    if (isFp(in.dst)) {
      unsigned x = gp(in.a, S0);
      unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
      a.movqToX(d, x);
      finF(in.dst, d);
    } else {
      unsigned x = fr(in.a, F0), d = dstG(in.dst, S0);
      a.movqFromX(d, x);
      if (!dbl(in.a)) a.mov32(d, d);
      finG(in.dst, d);
    }
    return;
  }
  case Op::Ctlz: {
    unsigned x = gp(in.a, S1);
    a.movImm(MV, ~uint64_t(0));
    a.bsr(S0, x);
    a.cmov(X::E, S0, MV);  // zero input: -1, so 63 - (-1) = 64
    a.movImm(S1, 63);
    a.sub(S1, S0);
    if (in.w < 64) a.subImm(S1, int32_t(64 - in.w));
    finG(in.dst, S1);
    return;
  }
  case Op::Cttz: {
    unsigned x = gp(in.a, S1);
    if (x != S1) a.mov(S1, x);
    if (in.w < 64) {
      a.movImm(MV, uint64_t(1) << in.w);
      a.or_(S1, MV);
    }
    a.movImm(MV, 64);
    a.bsf(S0, S1);
    a.cmov(X::E, S0, MV);
    finG(in.dst, S0);
    return;
  }
  case Op::Ctpop: {
    unsigned x = gp(in.a, S1), d = dstG(in.dst, S0);
    a.popcnt(d, x);
    finG(in.dst, d);
    return;
  }
  case Op::Bswap: {
    unsigned x = gp(in.a, S0);
    if (x != S0) a.mov(S0, x);
    a.bswap(S0);
    if (in.w < 64) a.shiftImm(5, S0, uint8_t(64 - in.w));
    finG(in.dst, S0);
    return;
  }
  case Op::Bitrev: {
    unsigned x = gp(in.a, S0);
    if (x != S0) a.mov(S0, x);
    static const uint64_t masks[3] = {0x5555555555555555ull, 0x3333333333333333ull, 0x0F0F0F0F0F0F0F0Full};
    for (unsigned k = 0; k < 3; k++) {
      uint8_t s = uint8_t(1u << k);
      a.movImm(MV, masks[k]);
      a.mov(S1, S0);
      a.shiftImm(5, S1, s);
      a.and_(S1, MV);
      a.and_(S0, MV);
      a.shiftImm(4, S0, s);
      a.or_(S0, S1);
    }
    a.bswap(S0);
    if (in.w < 64) a.shiftImm(5, S0, uint8_t(64 - in.w));
    finG(in.dst, S0);
    return;
  }
  case Op::Fshl:
  case Op::Fshr: {
    bool left = in.op == Op::Fshl;
    unsigned n = in.w;
    // a in rax, b in rdx, the amount in rcx.
    std::vector<Move> moves{{Place::reg_(S0, false), place(in.a)},
                            {Place::reg_(S2, false), place(in.b)},
                            {Place::reg_(S1, false), place(in.c)}};
    parallelMove(moves, *this);
    if (n == 64) {
      if (left) {
        a.shldCl(S0, S2);
      } else {
        a.shrdCl(S2, S0);
        a.mov(S0, S2);
      }
    } else {
      // a:b in 64 bits, shifted by s mod n.
      a.shiftImm(4, S0, uint8_t(n));
      a.or_(S0, S2);
      a.mov(MV, S0);
      a.mov(S0, S1);
      a.movImm(CY, n);
      a.xor_(S2, S2);
      a.div(CY);  // rdx = s mod n
      a.mov(S1, S2);
      a.mov(S0, MV);
      if (left) {
        a.shiftCl(4, S0);
        a.shiftImm(5, S0, uint8_t(n));
      } else {
        a.shiftCl(5, S0);
      }
      norm(S0, n);
    }
    finG(in.dst, S0);
    return;
  }
  case Op::FrameAddr: {
    unsigned d = dstG(in.dst, S0);
    a.mov(d, X::RBP);
    finG(in.dst, d);
    return;
  }
  case Op::Load: {
    unsigned size = in.aux;
    if (in.dst == None || al.loc[in.dst].k == Loc::None) return;  // nobody reads it
    auto [base, off] = addrOf(in.a, in.imm, S1);
    if (in.c != None) a.index(gp(in.c, S2), unsigned(in.imm2));
    if (isFp(in.dst)) {
      unsigned d = al.loc[in.dst].k == Loc::Reg ? al.loc[in.dst].reg : F0;
      a.loadF(d, base, d32(off), size == 8);
      a.noIndex();
      finF(in.dst, d);
      return;
    }
    unsigned d = dstG(in.dst, S0);
    a.load(d, base, d32(off), size);  // atomic loads are plain loads on x86
    a.noIndex();
    if (in.w == 1) a.andImm(d, 1);
    finG(in.dst, d);
    return;
  }
  case Op::Store: {
    unsigned size = in.aux;
    auto [base, off] = addrOf(in.a, in.imm, S1);
    if (in.c != None) {  // indexed (never atomic)
      unsigned ix = gp(in.c, S2);
      uint64_t bits;
      if (isFp(in.b)) {
        unsigned v = fr(in.b, F0);
        a.index(ix, unsigned(in.imm2));
        a.storeF(v, base, d32(off), size == 8);
      } else if (konst(in.b, bits) && (size < 8 || int64_t(bits) == int64_t(int32_t(bits)))) {
        a.index(ix, unsigned(in.imm2));
        a.storeImm(base, d32(off), size, int32_t(bits));
      } else {
        unsigned v = gp(in.b, S0);
        a.index(ix, unsigned(in.imm2));
        a.store(v, base, d32(off), size);
      }
      a.noIndex();
      return;
    }
    if (isFp(in.b)) {
      a.storeF(fr(in.b, F0), base, d32(off), size == 8);
      return;
    }
    uint64_t bits;
    if (in.flags & Atomic) {
      unsigned v = gp(in.b, S0);
      if (v != S0) a.mov(S0, v);
      a.xchg(S0, base, size);  // seq_cst (others are plain stores; xchg is correct for all)
      return;
    }
    if (konst(in.b, bits) && (size < 8 || int64_t(bits) == int64_t(int32_t(bits)))) {
      a.storeImm(base, d32(off), size, int32_t(bits));
      return;
    }
    a.store(gp(in.b, S0), base, d32(off), size);
    return;
  }
  case Op::Zero: {
    auto [base, off] = addrOf(in.a, in.imm, S1);
    zeroMem(base, off, in.aux2);
    return;
  }
  case Op::Copy8: {
    auto [db, doff] = addrOf(in.a, in.imm, S1);
    auto [sb, soff] = addrOf(in.b, in.imm2, S2);
    copyMem(db, doff, sb, soff, in.aux2);
    return;
  }
  case Op::MemCall: {
    std::vector<Move> moves{{Place::reg_(X::RDI, false), place(in.a)},
                            {Place::reg_(X::RSI, false), place(in.b)},
                            {Place::reg_(X::RDX, false), place(in.c)}};
    parallelMove(moves, *this);
    callSym(symName(in.aux ? "memset" : "memmove"));
    return;
  }
  case Op::Call: call(in); return;
  case Op::AtomicRMW: {
    unsigned size = in.w <= 8 ? 1 : in.w <= 16 ? 2 : in.w <= 32 ? 4 : 8;
    unsigned p = gp(in.a, S2);
    if (p != S2) a.mov(S2, p);
    unsigned v = gp(in.b, S0);
    if (v != S0) a.mov(S0, v);
    using B = LL::AtomicBinOp;
    switch (B(in.aux)) {
    case B::sub: a.neg(S0); [[fallthrough]];
    case B::add: a.lockXadd(S0, S2, size); break;
    case B::xchg: a.xchg(S0, S2, size); break;
    case B::_and:
    case B::_or:
    case B::_xor: {
      // Compare-and-swap loop: rax old, r11 new, rcx the operand.
      a.mov(S1, S0);
      a.load(S0, S2, 0, size);
      size_t loop = a.here();
      a.mov(MV, S0);
      if (B(in.aux) == B::_and) a.and_(MV, S1);
      if (B(in.aux) == B::_or) a.or_(MV, S1);
      if (B(in.aux) == B::_xor) a.xor_(MV, S1);
      a.lockCmpxchg(MV, S2, size);
      patch32(a.jcc(X::NE), int64_t(loop));
      break;
    }
    default: unsupported("atomicrmw operation");
    }
    norm(S0, in.w);
    finG(in.dst, S0);
    return;
  }
  case Op::CmpXchg: {
    unsigned size = in.w <= 8 ? 1 : in.w <= 16 ? 2 : in.w <= 32 ? 4 : 8;
    std::vector<Move> moves{{Place::reg_(S2, false), place(in.a)},
                            {Place::reg_(S0, false), place(in.b)},
                            {Place::reg_(MV, false), place(in.c)}};
    // r11 is the move scratch: the moves into it come last (a move never
    // needs the scratch to write a register).
    parallelMove(moves, *this);
    a.lockCmpxchg(MV, S2, size);  // rax <- old
    a.setcc(X::E, S1);
    a.movzx8(S1, S1);
    norm(S0, in.w);
    finG(in.dst, S0);
    finG(VId(in.extra), S1);
    return;
  }
  case Op::Fence: a.mfence(); return;
  case Op::Unreachable: a.ud2(); return;
  case Op::Ret: ret(in); return;
  case Op::Br: {
    edgeMoves(in.extra);
    uint32_t t = f.edges[in.extra].block;
    if (t != nextBlock) branchTo(t);
    return;
  }
  case Op::CondBr: {
    X::CC cc = X::NE;
    const Val &cv = f.vals[in.a];
    if (cv.def != ~0u && al.fused[cv.def]) {
      const Inst &ic = f.insts[cv.def];
      cmpFlags(ic);
      cc = icmpCC(LL::ICmpPredicate(ic.aux));
    } else {
      unsigned c = gp(in.a, S0);
      a.test(c, c);
    }
    const Edge &te = f.edges[in.extra], &fe = f.edges[in.aux2];
    uint32_t t = te.block, fb = fe.block;
    bool targs = te.argEnd > te.argBegin, fargs = fe.argEnd > fe.argBegin;
    if (!targs && !fargs && t == nextBlock) {
      jccTo(X::CC(cc ^ 1), fb);
    } else if (!targs) {
      jccTo(cc, t);
      edgeMoves(in.aux2);
      if (fb != nextBlock) branchTo(fb);
    } else if (!fargs) {
      jccTo(X::CC(cc ^ 1), fb);
      edgeMoves(in.extra);
      if (t != nextBlock) branchTo(t);
    } else {
      int lt = newLabel();
      jumpTo(true, cc, -1, lt);
      edgeMoves(in.aux2);
      branchTo(fb);
      bind(lt);
      edgeMoves(in.extra);
      if (t != nextBlock) branchTo(t);
    }
    return;
  }
  case Op::Switch: {
    const SwitchRec &sr = f.switches[in.extra];
    unsigned v = gp(in.a, S2);
    if (v != S2) a.mov(S2, v);  // edge moves may write v's register
    for (size_t k = 0; k + 1 < sr.edges.size(); k++) {
      uint64_t cv = sr.values[k];
      if (cv < (uint64_t(1) << 31)) {
        a.cmpImm(S2, int32_t(cv));
      } else {
        a.movImm(S1, cv);
        a.cmp(S2, S1);
      }
      uint32_t ei = sr.edges[k];
      const Edge &ed = f.edges[ei];
      if (ed.argEnd == ed.argBegin) {
        jccTo(X::E, ed.block);
        continue;
      }
      int ln = newLabel();
      jumpTo(true, X::NE, -1, ln);
      edgeMoves(ei);
      branchTo(ed.block);
      bind(ln);
    }
    uint32_t d = sr.edges.back();
    edgeMoves(d);
    if (f.edges[d].block != nextBlock) branchTo(f.edges[d].block);
    return;
  }
  default: unsupported("mir: operation");
  }
}

void Emitter::relax(size_t relocFrom, std::vector<ModuleGen::Row> &rows) {
  if (jumps.empty()) return;
  const uint64_t start = fnStart;
  const size_t n = jumps.size();
  for (const Jump &j : jumps)
    if (j.block >= 0 ? !emitted[size_t(j.block)] : labels[size_t(j.label)] < 0) unsupported("jump to code not emitted");
  auto longLen = [](const Jump &j) -> uint64_t { return j.cond ? 6 : 5; };
  auto target = [&](const Jump &j) -> uint64_t {
    return j.block >= 0 ? blockStart[size_t(j.block)] : uint64_t(labels[size_t(j.label)]);
  };
  std::vector<char> isShort(n, 0);
  std::vector<uint64_t> saved(n + 1, 0);
  auto prefix = [&] {
    for (size_t k = 0; k < n; k++) saved[k + 1] = saved[k] + (isShort[k] ? longLen(jumps[k]) - 2 : 0);
  };
  auto newPos = [&](uint64_t p) {
    size_t k = size_t(std::lower_bound(jumps.begin(), jumps.end(), p, [](const Jump &j, uint64_t q) { return j.at < q; }) -
                      jumps.begin());
    return p - saved[k];
  };
  for (bool changed = true; changed;) {
    changed = false;
    prefix();
    for (size_t k = 0; k < n; k++) {
      if (isShort[k]) continue;
      const Jump &j = jumps[k];
      uint64_t t = target(j);
      int64_t from = int64_t(newPos(j.at)) + 2;
      int64_t to = int64_t(newPos(t)) - (t > j.at ? int64_t(longLen(j) - 2) : 0);
      if (to - from >= -128 && to - from <= 127) {
        isShort[k] = 1;
        changed = true;
      }
    }
  }
  prefix();
  std::vector<uint8_t> &text = w.bytes(Sect::Text);
  std::vector<uint8_t> out;
  out.reserve(text.size() - start);
  uint64_t at = start;
  for (size_t k = 0; k < n; k++) {
    const Jump &j = jumps[k];
    out.insert(out.end(), text.begin() + int64_t(at), text.begin() + int64_t(j.at));
    int64_t from = int64_t(start + out.size());
    int64_t to = int64_t(newPos(target(j)));
    if (isShort[k]) {
      out.push_back(j.cond ? uint8_t(0x70 | j.cc) : 0xEB);
      out.push_back(uint8_t(int8_t(to - (from + 2))));
    } else {
      if (j.cond) {
        out.push_back(0x0F);
        out.push_back(uint8_t(0x80 | j.cc));
      } else {
        out.push_back(0xE9);
      }
      int32_t rel = int32_t(to - (from + int64_t(longLen(j))));
      for (int b = 0; b < 4; b++) out.push_back(uint8_t(uint32_t(rel) >> (8 * b)));
    }
    at = j.at + longLen(j);
  }
  out.insert(out.end(), text.begin() + int64_t(at), text.end());
  text.resize(start);
  text.insert(text.end(), out.begin(), out.end());
  w.remapTextRelocs(relocFrom, [&](uint64_t off) { return newPos(off); });
  for (auto &r : rows) std::get<0>(r) = newPos(start + std::get<0>(r)) - start;
}

// Register hints: arguments, results, parameters and return values where
// the calling convention puts them.
void Emitter::hints() {
  al.hint.assign(f.vals.size(), -1);
  auto set = [&](VId v, int r) {
    if (v != None && r >= 0 && al.hint[v] < 0) al.hint[v] = int16_t(r);
  };
  uint64_t n = 0;
  std::vector<ArgLoc> pl = assignArgsX64(TypeRange(fn.getFunctionType().getParams()), n, sretFn());
  for (size_t i = 0; i < pl.size() && i < f.params.size(); i++) set(f.params[i], pl[i].reg);
  auto results = [&](Type rt, const std::vector<VId> &vs) {
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(rt, 0, parts);
    unsigned ngp = 0, nfp = 0;
    for (size_t i = 0; i < parts.size() && i < vs.size(); i++) {
      Type t = parts[i].second;
      int r = isFloat(t) ? (nfp < 2 ? int(nfp) : -1) : (ngp < 3 ? int(retGpr[ngp]) : -1);
      isFloat(t) ? nfp++ : ngp++;
      set(vs[i], r);
    }
  };
  for (const CallRec &c : f.calls) {
    auto op = cast<LL::CallOp>(c.op);
    bool mem = c.sretObj >= 0;
    std::vector<ArgLoc> locs = assignArgsX64(op.getArgOperands().getTypes(), n, mem);
    for (size_t i = 0; i < locs.size() && i < c.args.size(); i++) set(c.args[i], locs[i].reg);
    if (!c.results.empty()) results(op.getResult().getType(), c.results);
  }
  Type rt = fn.getFunctionType().getReturnType();
  if (!isa<LL::LLVMVoidType>(rt) && !sretFn())
    for (const RetRec &r : f.rets) results(rt, r.pieces);
}

void Emitter::run() {
  mg.checkFunction(fn);
  build(mg, fn, f, true);
  hints();
  allocate(f, regs(), al);
  layout();
  fnStart = a.here();
  size_t relocFrom = w.checkpoint().rels;
  std::vector<ModuleGen::Row> rows;
  auto row = [&](Location l) {
    auto r = lineOf(l);
    if (!r) return;
    uint32_t file = mg.fileId(r->first);
    uint64_t at = a.here() - fnStart;
    if (!rows.empty() && std::get<1>(rows.back()) == r->second && std::get<2>(rows.back()) == file) return;
    if (!rows.empty() && std::get<0>(rows.back()) == at) rows.pop_back();
    rows.push_back({at, r->second, file});
  };
  row(fn.getLoc());
  std::map<std::string, uint64_t> ob;
  uint64_t was = a.here();
  prologue();
  if (opStatsOn) ob["prologue"] += a.here() - was;
  blockStart.assign(f.blocks.size(), 0);
  emitted.assign(f.blocks.size(), 0);
  for (uint32_t b = 0; b < f.blocks.size(); b++) {
    const BB &bl = f.blocks[b];
    if (bl.dead) continue;
    blockStart[b] = a.here();
    emitted[b] = 1;
    nextBlock = b + 1;
    while (nextBlock < f.blocks.size() && f.blocks[nextBlock].dead) nextBlock++;
    for (uint32_t i = bl.begin; i < bl.end; i++) {
      const Inst &in = f.insts[i];
      if (in.dead || al.fused[i]) continue;
      if (in.src) row(in.src->getLoc());
      was = a.here();
      emit(i);
      if (opStatsOn) ob[std::string("mir.") + std::to_string(unsigned(in.op))] += a.here() - was;
    }
  }
  if (opStatsOn) {
    std::lock_guard<std::mutex> l(opBytesMu);
    for (auto &[k, v] : ob) opBytesAll[k] += v;
  }
  relax(relocFrom, rows);
  auto lk = fn.getLinkage();
  std::string sym = symName(fn.getSymName());
  w.define(sym, Sect::Text, fnStart, !isLocalLinkage(lk), isWeakLinkage(lk), isHidden(fn));
  mg.lines.push_back({w.symbol(sym), a.here() - fnStart, std::move(rows), {}, {}});
}

} // namespace

bool mirX64(ModuleGen &mg, LL::LLVMFuncOp fn) {
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

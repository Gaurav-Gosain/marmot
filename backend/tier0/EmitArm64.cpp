// EmitArm64.cpp: the arm64 function emitter (Darwin, as LLVM lowers it), on
// the decisions of FuncPlan (Middle.h): values in their slots at sp +
// offset or in their home registers, integers zero extended in 64 bits.
#include "Abi.h"
#include "Arm64.h"
#include "Middle.h"

#include "mlir/Interfaces/SideEffectInterfaces.h"

#include <cstring>

namespace marmot {
namespace tier0 {
namespace {

using namespace a64;

// Scratch registers. x0-x7 and d0-d7 carry arguments; x9-x15 are free
// temporaries; x16/x17 are used for addresses of large offsets.
constexpr unsigned R1 = 9, R2 = 10, R3 = 11, R4 = 12, RA = 13, RT = 16;
constexpr unsigned F1 = 16, F2 = 17, F3 = 18;  // v16-v18: caller saved, not arguments

// ---- function level ----

class FuncGen : public FuncPlan {
public:
  FuncGen(ModuleGen &mg, LL::LLVMFuncOp f) : FuncPlan(mg, f), w(mg.w) {}
  void run();

private:
  uint64_t outgoingArgBytes(LL::CallOp c) override {
    uint64_t n = 0;
    assignArgs(c.getArgOperands().getTypes(), n);
    return n;
  }
  bool resultInMemory(Type t) override { return returnsInMemory(t); }

  MachOWriter &w;

  Block *nextBlock = nullptr;                  // the block emitted after the current one
  void addressOf(unsigned d, LL::AddressOfOp a);  // d = the global's address
  llvm::DenseMap<Block *, uint64_t> blockStart;

  struct Fixup {
    uint64_t at;
    Block *dest;
    enum { B, BCond, Cbnz, Cbz } kind;
    Cond cond;
    unsigned reg;
  };
  std::vector<Fixup> fixups;
  void allocate();

  // Instructions.
  void e(uint32_t w32) {
    w.emit32(w32);
    noteWrites(w32);
  }
  // The register cache: cache[r] is the sp offset whose 8 bytes register r
  // holds (-1: none), so a load of that slot into r is dropped. Every
  // instruction emitted drops the registers it writes; calls, stores to the
  // stack other than stAt's, sp changes, block starts and labels inside an
  // operation drop everything.
  int64_t cache[32];
  bool inStAt = false;
  void flushCache() {
    for (int64_t &c : cache) c = -1;
  }
  void noteWrites(uint32_t w32);
  void mv(unsigned d, unsigned s) {  // a 64-bit move, none to itself
    if (d != s) e(movReg(d, s));
  }
  uint64_t here() const { return w.size(Sect::Text); }
  void addrOf(unsigned rd, uint64_t off);  // rd = sp + off
  void ldSlot(unsigned r, uint64_t off, uint64_t size);
  void stSlot(unsigned r, uint64_t off, uint64_t size);
  void movImm(unsigned rd, uint64_t v);
  void copyBytes(unsigned dstBase, uint64_t dstOff, unsigned srcBase, uint64_t srcOff, uint64_t n);
  void memLoad(unsigned rt, unsigned base, uint64_t size);
  void memStore(unsigned rt, unsigned base, uint64_t size);
  // Load or store at [base + off] (base a register or sp), in one
  // instruction when the offset fits.
  void ldAt(unsigned rt, unsigned base, uint64_t off, uint64_t size);
  void stAt(unsigned rt, unsigned base, uint64_t off, uint64_t size);

  // Values.
  void get(unsigned r, Value v);            // scalar into a general register, canonical
  void getSigned(unsigned r, Value v);      // sign extended to 64 bits
  void put(unsigned r, Value v);            // stores a scalar, normalizing the width
  void putRaw(unsigned r, Value v);         // stores a scalar as it is
  // Register operands: src is v's home register, or scratch loaded with v;
  // dst is the register to compute v into (its home, or scratch); done
  // normalizes it and stores it when it is not the home.
  unsigned src(Value v, unsigned scratch) {
    if (auto h = home.find(v); h != home.end()) return h->second;
    get(scratch, v);
    return scratch;
  }
  unsigned srcSigned(Value v, unsigned scratch) {
    unsigned wdt = intWidth(v.getType());
    if (wdt >= 64) return src(v, scratch);
    getSigned(scratch, v);
    return scratch;
  }
  unsigned dst(Value v, unsigned scratch) {
    if (auto h = home.find(v); h != home.end()) return h->second;
    return scratch;
  }
  void done(unsigned r, Value v) { put(r, v); }
  void getF(unsigned vr, Value v);          // float into an FP register
  void putF(unsigned vr, Value v);
  void normalize(unsigned r, Type t);

  void emitOp(Operation &op);
  void cmpFlags(LL::ICmpOp c);  // sets the flags for c's predicate
  // The address in v as a register plus *off (a folded GEP's offset).
  unsigned addrReg(Value v, unsigned scratch, uint64_t &off) {
    off = 0;
    if (Operation *d = v.getDefiningOp())
      if (auto it = foldGep.find(d); it != foldGep.end()) {
        off = it->second;
        return src(cast<LL::GEPOp>(d).getBase(), scratch);
      }
    return src(v, scratch);
  }
  void emitCall(LL::CallOp call);
  void emitIntrinsic(LL::CallIntrinsicOp c);
  bool emitWide(Operation &op);
  void emitReturn(LL::ReturnOp ret);
  void stackAddr(unsigned rd, unsigned base, uint64_t off);  // rd = base + off
  // rd = rn + off with add/sub immediates (12 bits, and 12 bits shifted by
  // 12) when |off| is under 16 MiB, else through a constant in rd.
  void addConst(unsigned rd, unsigned rn, int64_t off);
  void zeroBytes(unsigned base, uint64_t off, uint64_t n);  // n zero bytes at [base + off]
  void edgeMoves(Block *dest, OperandRange args);
  void branchTo(Block *dest);
  void jumpIf(bool flags, Cond cc, unsigned reg, bool sense, Block *dest);
  void prologue();
  void epilogue();
  int64_t epiAt = -1; // offset of the function's epilogue, once emitted
  void saveRestore(bool save);
};

void FuncGen::noteWrites(uint32_t w) {
  // Calls: every caller saved register.
  if ((w & 0xFC000000u) == 0x94000000u || (w & 0xFFFFFC1Fu) == 0xD63F0000u) {
    flushCache();
    return;
  }
  // Branches and returns write no register.
  if ((w & 0x7C000000u) == 0x14000000u || (w & 0xFF000010u) == 0x54000000u || (w & 0x7E000000u) == 0x34000000u ||
      (w & 0xFFFFFC1Fu) == 0xD61F0000u || (w & 0xFFFFFC1Fu) == 0xD65F0000u)
    return;
  unsigned rt = w & 31, rn = (w >> 5) & 31;
  if (((w >> 25) & 5) == 4) {  // loads and stores
    bool single = ((w >> 28) & 3) == 3, pair = ((w >> 28) & 3) == 2;
    bool atomic = single && ((w >> 24) & 1) == 0 && ((w >> 21) & 1) == 1 && ((w >> 10) & 3) == 0;
    bool store = (single && !atomic && ((w >> 22) & 3) == 0) || (pair && ((w >> 22) & 1) == 0);
    if (store) {
      // To the stack outside stAt (stp, zeroing, saves): drop everything.
      // Stores through other registers do not reach value slots, except
      // the copies and zeroing that address them through x12/x13, which
      // drop the cache themselves.
      if (rn == SP && !inStAt) flushCache();
      bool wb = (pair && ((w >> 23) & 3) != 2) || (single && ((w >> 24) & 1) == 0 && ((w >> 21) & 1) == 0 && ((w >> 10) & 1) == 1);
      if (wb && rn == SP) flushCache();
      return;
    }
    // A load (or an exclusive or atomic access): the registers it may write.
    for (unsigned r : {rt, (w >> 10) & 31, (w >> 16) & 31, rn})
      if (r < 31) cache[r] = -1;
    if (rn == SP) {
      bool wb = (pair && ((w >> 23) & 3) != 2) || (single && ((w >> 24) & 1) == 0 && ((w >> 21) & 1) == 0 && ((w >> 10) & 1) == 1);
      if (wb) flushCache();
    }
    return;
  }
  // Everything else writes at most Rd (bits 0-4); 31 is sp for add and sub
  // with an immediate (and for add with an extended register).
  if (rt == 31) {
    // Without the S bit, Rd 31 of add/sub immediate or extended is sp.
    bool s = (w >> 29) & 1;
    if (!s && (((w >> 24) & 0x1F) == 0x11 || ((w >> 21) & 0xFF) == 0x59)) flushCache();
    return;
  }
  cache[rt] = -1;
}

void FuncGen::addrOf(unsigned rd, uint64_t off) {
  if (off < 4096) {
    e(addImm(rd, SP, uint32_t(off)));
  } else {
    if (off >= (uint64_t(1) << 24)) unsupported("frame larger than 16 MiB");
    e(addImm(rd, SP, uint32_t(off >> 12), true, true));
    e(addImm(rd, rd, uint32_t(off & 0xFFF)));
  }
}

void FuncGen::ldSlot(unsigned r, uint64_t off, uint64_t size) { ldAt(r, SP, off, size); }
void FuncGen::stSlot(unsigned r, uint64_t off, uint64_t size) { stAt(r, SP, off, size); }

void FuncGen::ldAt(unsigned r, unsigned base, uint64_t off, uint64_t size) {
  if (base == SP && size == 8 && r < 31 && cache[r] == int64_t(off)) return;  // r holds it
  unsigned ls = log2Size(size);
  if (off % size == 0 && (off >> ls) < 4096) {
    e(ldrImm(r, base, uint32_t(off), ls));
  } else if (off < 256) {
    e(ldur(r, base, int32_t(off), ls));
  } else {
    stackAddr(RT, base, off);
    e(ldrImm(r, RT, 0, ls));
  }
  if (base == SP && size == 8 && r < 31) cache[r] = int64_t(off);
}

void FuncGen::stAt(unsigned r, unsigned base, uint64_t off, uint64_t size) {
  unsigned ls = log2Size(size);
  inStAt = base == SP;
  if (off % size == 0 && (off >> ls) < 4096) {
    e(strImm(r, base, uint32_t(off), ls));
  } else if (off < 256) {
    e(stur(r, base, int32_t(off), ls));
  } else {
    stackAddr(RT, base, off);
    e(strImm(r, RT, 0, ls));
  }
  inStAt = false;
  if (base == SP) {
    // The slot changed: registers holding what overlapped it no longer do.
    for (int64_t &c : cache)
      if (c >= 0 && c < int64_t(off + size) && c + 8 > int64_t(off)) c = -1;
    if (size == 8 && r < 31) cache[r] = int64_t(off);
  }
}

void FuncGen::movImm(unsigned rd, uint64_t v) {
  if (~v < 0x10000) {
    e(movn(rd, uint32_t(~v & 0xFFFF), 0));
    return;
  }
  bool first = true;
  for (unsigned hw = 0; hw < 4; hw++) {
    uint32_t chunk = uint32_t(v >> (16 * hw)) & 0xFFFF;
    if (chunk == 0 && !(hw == 3 && first)) continue;
    e(first ? movz(rd, chunk, hw) : movk(rd, chunk, hw));
    first = false;
  }
  if (first) e(movz(rd, 0, 0));
}

// Copies n bytes from [srcBase + srcOff] to [dstBase + dstOff] (bases are
// registers, sp allowed) through x14 and x15: 16 bytes per ldp/stp, then 8,
// 4, 2, 1. An address is used as base + offset when every access fits the
// instructions' offsets, else it is made in x13 (source) or x12 (target).
void FuncGen::copyBytes(unsigned dstBase, uint64_t dstOff, unsigned srcBase, uint64_t srcOff, uint64_t n) {
  constexpr unsigned X14 = 14, X15 = 15;
  if (n > 32760) unsupported("aggregate copy over 32 KiB");
  auto base = [&](unsigned out, unsigned reg, uint64_t &off) -> unsigned {
    // Kept as base + offset when every access fits; a copy of 32 bytes or
    // more past ldp's reach is cheaper from a register pointing at it.
    if (off % 8 == 0 && off + n <= 32760 && (n < 32 || off + n <= 504)) return reg;
    if (reg == SP) {
      addrOf(out, off);
    } else if (off < 4096) {
      e(addImm(out, reg, uint32_t(off)));
    } else {
      movImm(out, off);
      e(addReg(out, out, reg));
    }
    off = 0;
    return out;
  };
  unsigned sb = base(RA, srcBase, srcOff);  // x13 first: dstBase is never x13
  unsigned db = base(R4, dstBase, dstOff);
  if (db != dstBase) flushCache();  // writes slots through x12
  uint64_t i = 0;
  for (; i + 16 <= n && srcOff + i + 8 <= 504 && dstOff + i + 8 <= 504; i += 16) {
    e(ldpOff(X14, X15, sb, int32_t(srcOff + i)));
    e(stpOff(X14, X15, db, int32_t(dstOff + i)));
  }
  for (uint64_t sz = 8; sz >= 1; sz /= 2)
    for (; i + sz <= n; i += sz) {
      ldAt(X14, sb, srcOff + i, sz);
      stAt(X14, db, dstOff + i, sz);
    }
}

void FuncGen::memLoad(unsigned rt, unsigned base, uint64_t size) { e(ldrImm(rt, base, 0, log2Size(size))); }
void FuncGen::memStore(unsigned rt, unsigned base, uint64_t size) { e(strImm(rt, base, 0, log2Size(size))); }

void FuncGen::normalize(unsigned r, Type t) {
  unsigned wdt = intWidth(t);
  if (wdt < 64) e(ubfx0(r, r, wdt));
}

void FuncGen::get(unsigned r, Value v) {
  if (sizeOf(v.getType()) > 8) unsupported("i128 operation");
  uint64_t bits;
  if (constBits(v, bits)) {
    movImm(r, bits);
    return;
  }
  if (auto h = home.find(v); h != home.end()) {
    if (h->second != r) mv(r, h->second);
    return;
  }
  ldSlot(r, of(v), 8);
}

void FuncGen::putRaw(unsigned r, Value v) {
  if (auto h = home.find(v); h != home.end()) {
    if (h->second != r) mv(h->second, r);
    return;
  }
  stSlot(r, of(v), 8);
}

void FuncGen::getSigned(unsigned r, Value v) {
  get(r, v);
  unsigned wdt = intWidth(v.getType());
  if (wdt < 64) e(sbfx0(r, r, wdt));
}

void FuncGen::put(unsigned r, Value v) {
  if (sizeOf(v.getType()) > 8) unsupported("i128 operation");
  if (!isFloat(v.getType())) normalize(r, v.getType());
  putRaw(r, v);
}

void FuncGen::getF(unsigned vr, Value v) {
  bool dbl = v.getType().isF64();
  e(fmovFromGp(vr, src(v, R1), dbl));
}

void FuncGen::putF(unsigned vr, Value v) {
  bool dbl = v.getType().isF64();
  unsigned d = dst(v, R1);
  e(fmovToGp(d, vr, dbl));  // a 32-bit fmov zeroes the upper half
  putRaw(d, v);
}

void FuncGen::prologue() {
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
  saveRestore(true);
  // Incoming arguments, split as LLVM splits them, into the entry block's
  // argument slots. Stack arguments start at the caller's sp, fp + 16.
  if (sret) stSlot(X8, sretSlot, 8);
  auto params = fn.getBody().front().getArguments();
  uint64_t stackSize = 0;
  for (const ArgLoc &l : assignArgs(TypeRange(fn.getFunctionType().getParams()), stackSize)) {
    BlockArgument a = params[l.arg];
    unsigned r = R1;
    if (l.reg < 0) {
      ldAt(R1, FP, 16 + l.stack, sizeOf(l.t));
    } else if (l.fp) {
      e(fmovToGp(R1, unsigned(l.reg), l.t.isF64()));
      if (l.t.isF32()) e(uxtw(R1, R1));
    } else if (intWidth(l.t) >= 64) {
      r = unsigned(l.reg);  // as it is: no other bits to clear
    } else {
      e(movReg(R1, unsigned(l.reg)));
    }
    if (!l.fp) normalize(r, l.t);  // an i1's other bits are undefined
    if (isAggregate(a.getType()))
      stAt(r, SP, of(a) + l.off, sizeOf(l.t));
    else
      putRaw(r, a);
  }
}

void FuncGen::stackAddr(unsigned rd, unsigned base, uint64_t off) {
  if (base == SP) {
    addrOf(rd, off);
  } else {
    addConst(rd, base, int64_t(off));
  }
}

void FuncGen::zeroBytes(unsigned base, uint64_t off, uint64_t n) {
  if (off + n > 4096 || (base != SP && off + n > 504 && n >= 32)) {
    // Out of the offsets' reach: from a register pointing at it.
    if (base == SP) flushCache();
    stackAddr(R4, base, off);
    base = R4;
    off = 0;
  }
  uint64_t i = 0;
  for (; i + 16 <= n && (off + i) % 8 == 0 && off + i + 8 <= 504; i += 16) e(stpOff(XZR, XZR, base, int32_t(off + i)));
  for (; i + 8 <= n; i += 8) stAt(XZR, base, off + i, 8);
  for (; i + 4 <= n; i += 4) stAt(XZR, base, off + i, 4);
  for (; i + 2 <= n; i += 2) stAt(XZR, base, off + i, 2);
  for (; i < n; i++) stAt(XZR, base, off + i, 1);
  if (base == SP)
    for (int64_t &c : cache)
      if (c >= 0 && c < int64_t(off + n) && c + 8 > int64_t(off)) c = -1;
}

void FuncGen::addConst(unsigned rd, unsigned rn, int64_t off) {
  uint64_t m = off < 0 ? uint64_t(-off) : uint64_t(off);
  if (m >= (uint64_t(1) << 24) || (rd == rn && off == 0)) {
    if (off == 0) return;
    unsigned t = rd == rn ? RT : rd;
    movImm(t, uint64_t(off));
    e(addReg(rd, rn, t));
    return;
  }
  uint32_t hi = uint32_t(m >> 12), lo = uint32_t(m & 0xFFF);
  unsigned from = rn;
  if (hi) {
    e(off < 0 ? subImm(rd, from, hi, true, true) : addImm(rd, from, hi, true, true));
    from = rd;
  }
  if (lo || from != rd)
    e(off < 0 ? subImm(rd, from, lo) : addImm(rd, from, lo));
}

// The callee saved registers that hold values, saved (or restored) two at a
// time where the offset allows.
void FuncGen::saveRestore(bool save) {
  std::vector<unsigned> regs;
  for (unsigned r = 19; r <= 28; r++)
    if (calleeUsed >> r & 1) regs.push_back(r);
  uint64_t at = saveArea;
  size_t i = 0;
  for (; i + 2 <= regs.size() && at + 8 <= 504; i += 2, at += 16)
    e(save ? stpOff(regs[i], regs[i + 1], SP, int32_t(at)) : ldpOff(regs[i], regs[i + 1], SP, int32_t(at)));
  for (; i < regs.size(); i++, at += 8) {
    if (save)
      stSlot(regs[i], at, 8);
    else
      ldSlot(regs[i], at, 8);
  }
}

void FuncGen::epilogue() {
  // One epilogue per function: later returns branch to the first (it is
  // the same: the callee-saved registers, the frame and the return).
  if (epiAt >= 0) {
    e(b(int32_t(epiAt - int64_t(here()))));
    return;
  }
  epiAt = int64_t(here());
  saveRestore(false);
  if (frameSize) e(movSp(SP, FP));  // with no frame, sp is still fp
  e(ldpPost(FP, LR, SP, 16));
  e(ret());
}

void FuncGen::edgeMoves(Block *dest, OperandRange args) {
  if (args.empty()) return;
  if (args.size() == 1 && !isAggregate(args[0].getType())) {
    if (auto h = home.find(dest->getArgument(0)); h != home.end()) {
      get(h->second, args[0]);  // straight into the argument's register
      return;
    }
    putRaw(src(args[0], R1), dest->getArgument(0));
    return;
  }
  // When no source is one of dest's own arguments, nothing is overwritten
  // before it is read: move directly.
  bool direct = true;
  for (Value v : args)
    if (auto ba = dyn_cast<BlockArgument>(v); ba && ba.getOwner() == dest) direct = false;
  if (direct) {
    for (unsigned i = 0; i < args.size(); i++) {
      Value v = args[i];
      BlockArgument a = dest->getArgument(i);
      if (isAggregate(a.getType())) {
        copyBytes(SP, of(a), SP, of(v), std::max<uint64_t>((sizeOf(a.getType()) + 7) & ~uint64_t(7), 8));
      } else if (auto h = home.find(a); h != home.end()) {
        get(h->second, v);
      } else {
        putRaw(src(v, R1), a);
      }
    }
    return;
  }
  // Parallel move: sources to the temporary area, then to the arguments.
  uint64_t t = tempArea;
  std::vector<uint64_t> tmp;
  for (Value v : args) {
    uint64_t sz = std::max<uint64_t>((sizeOf(v.getType()) + 7) & ~uint64_t(7), 8);
    if (isAggregate(v.getType())) {
      copyBytes(SP, t, SP, of(v), sz);
    } else {
      get(R1, v);
      stSlot(R1, t, 8);
    }
    tmp.push_back(t);
    t += sz;
  }
  for (unsigned i = 0; i < args.size(); i++) {
    BlockArgument a = dest->getArgument(i);
    uint64_t sz = std::max<uint64_t>((sizeOf(a.getType()) + 7) & ~uint64_t(7), 8);
    if (isAggregate(a.getType())) {
      copyBytes(SP, of(a), SP, tmp[i], sz);
    } else {
      ldSlot(R1, tmp[i], 8);
      putRaw(R1, a);
    }
  }
}

void FuncGen::branchTo(Block *dest) {
  fixups.push_back({here(), dest, Fixup::B, AL, 0});
  e(b(0));
}

// Jumps to dest when the condition is sense: on the flags (cc) or on reg.
void FuncGen::jumpIf(bool flags, Cond cc, unsigned reg, bool sense, Block *dest) {
  if (flags)
    fixups.push_back({here(), dest, Fixup::BCond, sense ? cc : Cond(cc ^ 1), 0});
  else
    fixups.push_back({here(), dest, sense ? Fixup::Cbnz : Fixup::Cbz, AL, reg});
  e(0);
}

void FuncGen::emitCall(LL::CallOp call) {
  if (call.getVarCalleeType()) unsupported("variadic call");
  auto args = call.getArgOperands();
  uint64_t stackSize = 0;
  std::vector<ArgLoc> locs = assignArgs(args.getTypes(), stackSize);
  auto load = [&](unsigned r, const ArgLoc &l) {
    Value v = args[l.arg];
    if (auto la = lazyAgg.find(v); la != lazyAgg.end()) {
      auto f = la->second.find(l.off);
      if (f == la->second.end()) e(movz(r, 0, 0));
      else if (auto ao = f->second.getDefiningOp<LL::AddressOfOp>()) addressOf(r, ao);
      else get(r, f->second);
      return;
    }
    if (auto lv = v.getDefiningOp<LL::LoadOp>(); lv && lazyLoad.count(lv)) {
      uint64_t ao;
      unsigned ar = addrReg(lv.getAddr(), R2, ao);
      ldAt(r, ar, ao + l.off, sizeOf(l.t));
    } else if (isAggregate(v.getType())) {
      ldAt(r, SP, of(v) + l.off, sizeOf(l.t));
    } else if (mg.signExtArg(call, l.arg)) {
      getSigned(r, v);
    } else {
      get(r, v);
    }
  };
  // Stack and floating point pieces first, through x9 and x10.
  for (const ArgLoc &l : locs) {
    if (l.reg >= 0 && !l.fp) continue;
    load(R1, l);
    if (l.reg >= 0) {
      e(fmovFromGp(unsigned(l.reg), R1, l.t.isF64()));
    } else {
      stAt(R1, SP, l.stack, sizeOf(l.t));
    }
  }
  // Indirect callee next (into x17, not an argument register).
  bool indirect = !call.getCallee();
  if (indirect) get(17, call.getCalleeOperands().front());
  for (size_t i = 0; i < locs.size(); i++) {
    const ArgLoc &l = locs[i];
    if (l.reg < 0 || l.fp) continue;
    // Two 8-byte pieces of one aggregate, side by side, into consecutive
    // registers: one ldp.
    if (i + 1 < locs.size()) {
      const ArgLoc &m = locs[i + 1];
      Value v = args[l.arg];
      if (m.arg == l.arg && m.reg == l.reg + 1 && !m.fp && m.off == l.off + 8 && sizeOf(l.t) == 8 &&
          sizeOf(m.t) == 8 && isAggregate(v.getType()) && !lazyAgg.count(v)) {
        unsigned base = SP;
        uint64_t off = 0;
        if (auto lv = v.getDefiningOp<LL::LoadOp>(); lv && lazyLoad.count(lv)) {
          uint64_t ao;
          base = addrReg(lv.getAddr(), R2, ao);
          off = ao + l.off;
        } else {
          off = of(v) + l.off;
        }
        if (off % 8 == 0 && off <= 496) {
          e(ldpOff(unsigned(l.reg), unsigned(m.reg), base, int32_t(off)));
          i++;
          continue;
        }
      }
    }
    load(unsigned(l.reg), l);
  }
  bool memResult = call.getNumResults() == 1 && returnsInMemory(call.getResult().getType());
  if (memResult) addrOf(X8, of(call.getResult()));
  auto site = [&] {
    if (auto m = callMap.find(call.getOperation()); m != callMap.end()) smSites.push_back({here() - fnStart, m->second});
  };
  if (indirect) {
    e(blr(17));
    site();
  } else {
    StringRef callee = *call.getCallee();
    if (callee == "marmot_gpu_for") {
      // CPU path: the call returns false.
      e(movz(0, 0, 0));
    } else {
      uint64_t at = here();
      e(bl(0));
      w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol(machoName(callee)));
      site();
    }
  }
  if (call.getNumResults() == 0 || memResult) return;
  Value r = call.getResult();
  std::vector<std::pair<uint64_t, Type>> parts;
  flatten(r.getType(), 0, parts);
  unsigned ngp = 0, nfp = 0;
  // Two adjacent 8-byte integer pieces of an aggregate (p and p+1).
  auto pairAt = [&](size_t p) {
    if (p + 1 >= parts.size()) return false;
    auto [o1, t1] = parts[p];
    auto [o2, t2] = parts[p + 1];
    return !isFloat(t1) && !isFloat(t2) && sizeOf(t1) == 8 && sizeOf(t2) == 8 && intWidth(t1) >= 64 &&
           intWidth(t2) >= 64 && o2 == o1 + 8;
  };
  for (size_t p = 0; p < parts.size(); p++) {
    auto [off, t] = parts[p];
    if (isFloat(t) ? nfp >= 8 : ngp >= 8) unsupported("result returned in memory");
    if (isAggregate(r.getType()) && ngp + 1 < 8 && pairAt(p) && (of(r) + off) % 8 == 0 && of(r) + off <= 496) {
      e(stpOff(ngp, ngp + 1, SP, int32_t(of(r) + off)));
      ngp += 2;
      p++;
      continue;
    }
    if (!isAggregate(r.getType())) {
      unsigned d = dst(r, R1);
      if (isFloat(t)) {
        e(fmovToGp(d, nfp++, t.isF64()));
      } else {
        mv(d, ngp++);
        normalize(d, t);
      }
      putRaw(d, r);
      continue;
    }
    if (isFloat(t)) {
      e(fmovToGp(R1, nfp++, t.isF64()));
      if (t.isF32()) e(uxtw(R1, R1));
    } else if (intWidth(t) >= 64) {
      stAt(ngp++, SP, of(r) + off, sizeOf(t));  // an aggregate: the register as it is
      continue;
    } else {
      mv(R1, ngp++);
      normalize(R1, t);
    }
    if (isAggregate(r.getType()))
      stAt(R1, SP, of(r) + off, sizeOf(t));
    else
      putRaw(R1, r);
  }
}

void FuncGen::emitReturn(LL::ReturnOp ret) {
  if (sret) {
    Value v = ret.getOperand(0);
    ldSlot(R1, sretSlot, 8);
    if (auto lv = v.getDefiningOp<LL::LoadOp>(); lv && lazyLoad.count(lv))
    {
      uint64_t ao;
      unsigned ar = addrReg(lv.getAddr(), R2, ao);
      copyBytes(R1, 0, ar, ao, sizeOf(v.getType()));
    }
    else
      copyBytes(R1, 0, SP, of(v), sizeOf(v.getType()));
  } else if (ret.getNumOperands() == 1) {
    Value v = ret.getOperand(0);
    std::vector<std::pair<uint64_t, Type>> parts;
    flatten(v.getType(), 0, parts);
    unsigned ngp = 0, nfp = 0;
    for (size_t p = 0; p < parts.size(); p++) {
      auto [off, t] = parts[p];
      if (isFloat(t) ? nfp >= 8 : ngp >= 8) unsupported("result returned in memory");
      // Two adjacent 8-byte integer pieces: one ldp.
      if (isAggregate(v.getType()) && ngp + 1 < 8 && p + 1 < parts.size()) {
        auto [o2, t2] = parts[p + 1];
        if (!isFloat(t) && !isFloat(t2) && sizeOf(t) == 8 && sizeOf(t2) == 8 && o2 == off + 8) {
          unsigned base = SP;
          uint64_t at;
          if (auto lv = v.getDefiningOp<LL::LoadOp>(); lv && lazyLoad.count(lv)) {
            uint64_t ao;
            base = addrReg(lv.getAddr(), R2, ao);
            at = ao + off;
          } else {
            at = of(v) + off;
          }
          if (at % 8 == 0 && at <= 496) {
            e(ldpOff(ngp, ngp + 1, base, int32_t(at)));
            ngp += 2;
            p++;
            continue;
          }
        }
      }
      if (auto lv = v.getDefiningOp<LL::LoadOp>(); lv && lazyLoad.count(lv)) {
        uint64_t ao;
        unsigned ar = addrReg(lv.getAddr(), R2, ao);
        if (!isFloat(t)) {
          ldAt(ngp++, ar, ao + off, sizeOf(t));
          continue;
        }
        ldAt(R1, ar, ao + off, sizeOf(t));
      } else if (isAggregate(v.getType())) {
        if (!isFloat(t)) {
          ldAt(ngp++, SP, of(v) + off, sizeOf(t));
          continue;
        }
        ldAt(R1, SP, of(v) + off, sizeOf(t));
      } else if (ModuleGen::signExtResult(fn)) {
        getSigned(R1, v);
      } else {
        get(R1, v);
      }
      if (isFloat(t))
        e(fmovFromGp(nfp++, R1, t.isF64()));
      else
        mv(ngp++, R1);
    }
  }
  epilogue();
}

void FuncGen::cmpFlags(LL::ICmpOp c) {
  bool sgn = isSignedPred(c.getPredicate());
  unsigned a = sgn ? srcSigned(c.getLhs(), R1) : src(c.getLhs(), R1);
  uint64_t bits;
  if (constBits(c.getRhs(), bits)) {
    unsigned wdt = intWidth(c.getRhs().getType());
    // The constant as the compare sees it: sign extended for a signed one.
    int64_t v = wdt < 64 && sgn ? int64_t(bits << (64 - wdt)) >> (64 - wdt) : int64_t(bits);
    if (v >= 0 && v < 4096 && (sgn || bits < 4096)) {
      e(cmpImm(a, uint32_t(v)));
      return;
    }
  }
  unsigned b = sgn ? srcSigned(c.getRhs(), R2) : src(c.getRhs(), R2);
  e(cmpReg(a, b));
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

// i128 values live in 16-byte slots (low word first) and are supported for
// local arithmetic, the shape math/bits produces.
bool FuncGen::emitWide(Operation &op) {
  bool wide = false;
  for (Type t : op.getOperandTypes()) wide |= isWide(t);
  for (Type t : op.getResultTypes()) wide |= isWide(t);
  if (!wide) return false;
  auto lo = [&](unsigned r, Value v) {
    if (!isWide(v.getType())) return get(r, v);
    ldSlot(r, of(v), 8);
  };
  auto hi = [&](unsigned r, Value v) { ldSlot(r, of(v) + 8, 8); };
  auto store = [&](unsigned l, unsigned h, Value v) {
    stSlot(l, of(v), 8);
    stSlot(h, of(v) + 8, 8);
  };
  auto pair = [&](auto encLo, auto encHi) {
    lo(R1, op.getOperand(0));
    hi(R2, op.getOperand(0));
    lo(R3, op.getOperand(1));
    hi(R4, op.getOperand(1));
    e(encLo(R1, R1, R3));
    e(encHi(R2, R2, R4));
    store(R1, R2, op.getResult(0));
  };
  constexpr unsigned X14 = 14, X15 = 15;
  // Shift of a:b (R1 low, R2 high) by R3 in [0, 128).
  auto shift = [&](bool left, bool arith) {
    lo(R1, op.getOperand(0));
    hi(R2, op.getOperand(0));
    lo(R3, op.getOperand(1));
    movImm(R4, 63);
    e(eorReg(RA, R3, R4));  // 63 - s for s < 64
    movImm(X14, 1);
    if (!left) {
      // small: lo = lo >> s | (hi << 1) << (63 - s); hi = hi >> s
      e(lsrv(X15, R1, R3));
      e(lslv(X14, R2, X14));
      e(lslv(X14, X14, RA));
      e(orrReg(X15, X15, X14));            // small lo
      e(arith ? asrv(X14, R2, R3) : lsrv(X14, R2, R3));  // small hi, also big lo (shift mod 64)
      movImm(RA, 63);
      if (arith) e(asrv(RA, R2, RA)); else movImm(RA, 0);  // big hi
      e(cmpImm(R3, 64));
      e(csel(R1, X14, X15, HS));
      e(csel(R2, RA, X14, HS));
    } else {
      // small: hi = hi << s | (lo >> 1) >> (63 - s); lo = lo << s
      e(lslv(X15, R2, R3));
      e(lsrv(X14, R1, X14));
      e(lsrv(X14, X14, RA));
      e(orrReg(X15, X15, X14));            // small hi
      e(lslv(X14, R1, R3));                // small lo, also big hi
      e(cmpImm(R3, 64));
      e(csel(R2, X14, X15, HS));
      e(csel(R1, XZR, X14, HS));
    }
    store(R1, R2, op.getResult(0));
  };
  llvm::TypeSwitch<Operation *>(&op)
      .Case<LL::ConstantOp>([&](auto c) {
        auto ia = dyn_cast<IntegerAttr>(c.getValue());
        if (!ia) unsupported("i128 constant");
        APInt v = ia.getValue().zextOrTrunc(128);
        movImm(R1, v.extractBitsAsZExtValue(64, 0));
        movImm(R2, v.extractBitsAsZExtValue(64, 64));
        store(R1, R2, c.getResult());
      })
      .Case<LL::ZExtOp>([&](auto c) {
        get(R1, c.getArg());
        store(R1, XZR, c.getResult());
      })
      .Case<LL::SExtOp>([&](auto c) {
        getSigned(R1, c.getArg());
        movImm(R3, 63);
        e(asrv(R2, R1, R3));
        store(R1, R2, c.getResult());
      })
      .Case<LL::TruncOp>([&](auto c) {
        if (isWide(c.getResult().getType())) unsupported("i128 operation");
        lo(R1, c.getArg());
        put(R1, c.getResult());
      })
      .Case<LL::AddOp>([&](auto) { pair(adds, adc); })
      .Case<LL::SubOp>([&](auto) { pair(subs, sbc); })
      .Case<LL::AndOp>([&](auto) {
        pair([](unsigned d, unsigned n, unsigned m) { return andReg(d, n, m); },
             [](unsigned d, unsigned n, unsigned m) { return andReg(d, n, m); });
      })
      .Case<LL::OrOp>([&](auto) {
        pair([](unsigned d, unsigned n, unsigned m) { return orrReg(d, n, m); },
             [](unsigned d, unsigned n, unsigned m) { return orrReg(d, n, m); });
      })
      .Case<LL::XOrOp>([&](auto) {
        pair([](unsigned d, unsigned n, unsigned m) { return eorReg(d, n, m); },
             [](unsigned d, unsigned n, unsigned m) { return eorReg(d, n, m); });
      })
      .Case<LL::MulOp>([&](auto) {
        lo(R1, op.getOperand(0));
        hi(R2, op.getOperand(0));
        lo(R3, op.getOperand(1));
        hi(R4, op.getOperand(1));
        e(umulh(RA, R1, R3));
        e(madd(RA, R1, R4, RA));
        e(madd(RA, R2, R3, RA));
        e(mul(R1, R1, R3));
        store(R1, RA, op.getResult(0));
      })
      .Case<LL::LShrOp>([&](auto) { shift(false, false); })
      .Case<LL::AShrOp>([&](auto) { shift(false, true); })
      .Case<LL::ShlOp>([&](auto) { shift(true, false); })
      .Default([&](Operation *o) { unsupported("i128 " + o->getName().getStringRef().str()); });
  return true;
}

void FuncGen::emitOp(Operation &op) {
  if (emitWide(op)) return;
  auto bin = [&](auto enc) {
    unsigned a = src(op.getOperand(0), R1), b = src(op.getOperand(1), R2), d = dst(op.getResult(0), R1);
    e(enc(d, a, b));
    done(d, op.getResult(0));
  };
  auto fbin = [&](auto enc) {
    bool dbl = op.getResult(0).getType().isF64();
    getF(F1, op.getOperand(0));
    getF(F2, op.getOperand(1));
    e(enc(F1, F1, F2, dbl));
    putF(F1, op.getResult(0));
  };
  llvm::TypeSwitch<Operation *>(&op)
      .Case<LL::ConstantOp>([&](auto c) {
        Type t = c.getType();
        uint64_t bits = 0;
        if (auto i = dyn_cast<IntegerAttr>(c.getValue()))
          bits = i.getValue().getZExtValue();
        else if (auto f = dyn_cast<FloatAttr>(c.getValue()))
          bits = f.getValue().bitcastToAPInt().getZExtValue();
        else
          unsupported("constant kind");
        if (isAggregate(t)) unsupported("aggregate constant");
        (void)bits;  // made at each use (constBits)
      })
      .Case<LL::ZeroOp, LL::UndefOp, LL::PoisonOp>([&](auto z) {
        Type t = z.getType();
        if (!isAggregate(t) && sizeOf(t) <= 8) return;  // made at each use
        if (lazyChainOps.count(z.getOperation())) return;  // made at the call
        if (!isa<LL::ZeroOp>(z.getOperation())) return;  // undef: any bits (as LLVM)
        if (storedZero(z.getResult())) return;  // each store writes the zeros
        uint64_t n = sizeOf(t);
        n = std::max<uint64_t>((n + 7) & ~uint64_t(7), 8);
        uint64_t at = of(z.getResult()), i = 0;
        unsigned base = SP;
        if (at + 8 > 504 && n >= 32) {  // out of stp's reach: through x12
          flushCache();
          addrOf(R4, at);
          base = R4;
          at = 0;
        }
        for (; i + 16 <= n && at + i + 8 <= 504; i += 16) e(stpOff(XZR, XZR, base, int32_t(at + i)));
        for (; i < n; i += 8) stAt(XZR, base, at + i, 8);
      })
      .Case<LL::AddressOfOp>([&](auto a) {
        if (lazyChainOps.count(a.getOperation())) return;  // made at the call
        unsigned d = dst(a.getResult(), R1);
        addressOf(d, a);
        putRaw(d, a.getResult());
      })
      .Case<LL::AddOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return addReg(d, n, m); }); })
      .Case<LL::SubOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return subReg(d, n, m); }); })
      .Case<LL::MulOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return mul(d, n, m); }); })
      .Case<LL::AndOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return andReg(d, n, m); }); })
      .Case<LL::OrOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return orrReg(d, n, m); }); })
      .Case<LL::XOrOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return eorReg(d, n, m); }); })
      .Case<LL::ShlOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return lslv(d, n, m); }); })
      .Case<LL::LShrOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return lsrv(d, n, m); }); })
      .Case<LL::UDivOp>([&](auto) { bin([](unsigned d, unsigned n, unsigned m) { return udiv(d, n, m); }); })
      .Case<LL::AShrOp>([&](auto o) {
        getSigned(R1, o.getLhs());
        get(R2, o.getRhs());
        e(asrv(R1, R1, R2));
        put(R1, o.getResult());
      })
      .Case<LL::SDivOp>([&](auto o) {
        getSigned(R1, o.getLhs());
        getSigned(R2, o.getRhs());
        e(sdiv(R1, R1, R2));
        put(R1, o.getResult());
      })
      .Case<LL::SRemOp>([&](auto o) {
        getSigned(R1, o.getLhs());
        getSigned(R2, o.getRhs());
        e(sdiv(R3, R1, R2));
        e(msub(R1, R3, R2, R1));
        put(R1, o.getResult());
      })
      .Case<LL::URemOp>([&](auto o) {
        get(R1, o.getLhs());
        get(R2, o.getRhs());
        e(udiv(R3, R1, R2));
        e(msub(R1, R3, R2, R1));
        put(R1, o.getResult());
      })
      .Case<LL::ICmpOp>([&](auto c) {
        if (fusedCmp.count(c.getOperation())) return;  // compared at the cond_br
        cmpFlags(c);
        unsigned d = dst(c.getResult(), R1);
        e(cset(d, icmpCond(c.getPredicate())));
        putRaw(d, c.getResult());
      })
      .Case<LL::SelectOp>([&](auto s) {
        Type t = s.getType();
        if (unsigned c = src(s.getCondition(), R3); c != R3) mv(R3, c);
        if (isAggregate(t)) {
          // Copy whichever value the condition picks.
          uint64_t sz = std::max<uint64_t>((sizeOf(t) + 7) & ~uint64_t(7), 8);
          uint64_t at = here();
          e(cbz(R3, 0));
          copyBytes(SP, of(s.getResult()), SP, of(s.getTrueValue()), sz);
          uint64_t skip = here();
          e(b(0));
          uint64_t elseAt = here();
          flushCache();
          copyBytes(SP, of(s.getResult()), SP, of(s.getFalseValue()), sz);
          uint64_t end = here();
          flushCache();  // the true path arrives here too
          auto patch = [&](uint64_t pos, uint32_t word) { std::memcpy(&w.bytes(Sect::Text)[pos], &word, 4); };
          patch(at, cbz(R3, int32_t(elseAt - at)));
          patch(skip, b(int32_t(end - skip)));
          return;
        }
        unsigned a = src(s.getTrueValue(), R1), b = src(s.getFalseValue(), R2), d = dst(s.getResult(), R1);
        e(cmpImm(R3, 0));
        e(csel(d, a, b, NE));
        putRaw(d, s.getResult());
      })
      .Case<LL::ZExtOp, LL::TruncOp, LL::PtrToIntOp, LL::IntToPtrOp, LL::AddrSpaceCastOp>([&](auto c) {
        if (fusedCmp.count(c.getOperation())) return;  // tested at the cond_br
        unsigned a = src(c.getArg(), R1), d = dst(c.getResult(), R1);
        if (intWidth(c.getResult().getType()) < intWidth(c.getArg().getType())) {
          e(ubfx0(d, a, intWidth(c.getResult().getType())));  // trunc: keep the low bits
          putRaw(d, c.getResult());
          return;
        }
        if (d != a) mv(d, a);
        putRaw(d, c.getResult());  // zext and pointer casts: already canonical
      })
      .Case<LL::SExtOp>([&](auto c) {
        getSigned(R1, c.getArg());
        put(R1, c.getResult());
      })
      .Case<LL::BitcastOp>([&](auto c) {
        if (isAggregate(c.getType())) unsupported("aggregate bitcast");
        get(R1, c.getArg());
        putRaw(R1, c.getResult());
      })
      .Case<LL::FAddOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fadd(d, n, m, x); }); })
      .Case<LL::MaximumOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fmax(d, n, m, x); }); })
      .Case<LL::MinimumOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fmin(d, n, m, x); }); })
      .Case<LL::MaxNumOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fmaxnm(d, n, m, x); }); })
      .Case<LL::MinNumOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fminnm(d, n, m, x); }); })
      .Case<LL::FSubOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fsub(d, n, m, x); }); })
      .Case<LL::FMulOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fmul(d, n, m, x); }); })
      .Case<LL::FDivOp>([&](auto) { fbin([](unsigned d, unsigned n, unsigned m, bool x) { return fdiv(d, n, m, x); }); })
      .Case<LL::FNegOp>([&](auto o) {
        bool dbl = o.getType().isF64();
        getF(F1, o.getOperand());
        e(fneg(F1, F1, dbl));
        putF(F1, o.getResult());
      })
      .Case<LL::FCmpOp>([&](auto c) {
        bool dbl = c.getLhs().getType().isF64();
        getF(F1, c.getLhs());
        getF(F2, c.getRhs());
        e(fcmp(F1, F2, dbl));
        using P = LL::FCmpPredicate;
        auto one = [&](Cond k) { e(cset(R1, k)); };
        auto either = [&](Cond k1, Cond k2) {
          e(cset(R1, k1));
          e(cset(R2, k2));
          e(orrReg(R1, R1, R2));
        };
        switch (c.getPredicate()) {
        case P::_false: e(movz(R1, 0, 0)); break;
        case P::_true: e(movz(R1, 1, 0)); break;
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
        put(R1, c.getResult());
      })
      .Case<LL::SIToFPOp, LL::UIToFPOp>([&](auto c) {
        bool dbl = c.getType().isF64();
        bool sgn = isa<LL::SIToFPOp>(c.getOperation());
        if (sgn)
          getSigned(R1, c.getArg());
        else
          get(R1, c.getArg());
        e(sgn ? scvtf(F1, R1, dbl) : ucvtf(F1, R1, dbl));
        putF(F1, c.getResult());
      })
      .Case<LL::FPToSIOp, LL::FPToUIOp>([&](auto c) {
        bool dbl = c.getArg().getType().isF64();
        getF(F1, c.getArg());
        e(isa<LL::FPToSIOp>(c.getOperation()) ? fcvtzs(R1, F1, dbl) : fcvtzu(R1, F1, dbl));
        put(R1, c.getResult());
      })
      .Case<LL::FPExtOp>([&](auto c) {
        getF(F1, c.getArg());
        e(fcvtStoD(F1, F1));
        putF(F1, c.getResult());
      })
      .Case<LL::FPTruncOp>([&](auto c) {
        getF(F1, c.getArg());
        e(fcvtDtoS(F1, F1));
        putF(F1, c.getResult());
      })
      .Case<LL::LoadOp>([&](auto l) {
        if (lazyLoad.count(l.getOperation())) return;  // read at each use
        Type t = l.getType();
        uint64_t ao;
        unsigned ar = addrReg(l.getAddr(), R2, ao);
        if (isAggregate(t)) {
          copyBytes(SP, of(l.getResult()), ar, ao, sizeOf(t));
          return;
        }
        uint64_t sz = sizeOf(t);
        unsigned d = dst(l.getResult(), R1);
        if (l.getOrdering() != LL::AtomicOrdering::not_atomic)
          e(ldar(d, ar, log2Size(sz)));  // never a folded address
        else
          ldAt(d, ar, ao, sz);
        if (intWidth(t) == 1) e(ubfx0(d, d, 1));
        putRaw(d, l.getResult());
      })
      .Case<LL::StoreOp>([&](auto s) {
        Type t = s.getValue().getType();
        uint64_t so;
        unsigned ar = addrReg(s.getAddr(), R2, so);
        if (isAggregate(t)) {
          if (storedZero(s.getValue())) {
            zeroBytes(ar, so, sizeOf(t));
            return;
          }
          if (auto lv = s.getValue().template getDefiningOp<LL::LoadOp>(); lv && lazyLoad.count(lv)) {
            uint64_t lo;
            unsigned lr = addrReg(lv.getAddr(), R3, lo);
            copyBytes(ar, so, lr, lo, sizeOf(t));
          } else {
            copyBytes(ar, so, SP, of(s.getValue()), sizeOf(t));
          }
          return;
        }
        uint64_t bits;
        unsigned v = constBits(s.getValue(), bits) && bits == 0 ? XZR : src(s.getValue(), R1);
        uint64_t sz = sizeOf(t);
        if (s.getOrdering() != LL::AtomicOrdering::not_atomic)
          e(stlr(v, ar, log2Size(sz)));  // never a folded address
        else
          stAt(v, ar, so, sz);
      })
      .Case<LL::AllocaOp>([&](auto a) {
        unsigned d = dst(a.getResult(), R1);
        addrOf(d, allocaStorage[a.getOperation()]);
        putRaw(d, a.getResult());
      })
      .Case<LL::GEPOp>([&](auto g) {
        if (foldGep.count(g.getOperation())) return;  // folded into its loads and stores
        unsigned acc = src(g.getBase(), R1);  // the address so far
        unsigned d = dst(g.getResult(), R1);
        Type et = g.getElemType();
        bool first = true;
        int64_t constOff = 0;
        for (auto idx : g.getIndices()) {
          Type cur = et;
          uint64_t scale;
          if (first) {
            scale = sizeOf(et);
          } else if (auto s = dyn_cast<LL::LLVMStructType>(et)) {
            auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx);
            if (!ia) unsupported("dynamic struct index");
            constOff += int64_t(fieldOffset(s, unsigned(ia.getInt())));
            et = s.getBody()[ia.getInt()];
            continue;
          } else if (auto at = dyn_cast<LL::LLVMArrayType>(et)) {
            scale = sizeOf(at.getElementType());
            et = at.getElementType();
          } else {
            unsupported("gep into a scalar");
          }
          (void)cur;
          first = false;
          if (auto ia = llvm::dyn_cast_if_present<IntegerAttr>(idx)) {
            constOff += ia.getInt() * int64_t(scale);
          } else {
            Value iv = llvm::cast<Value>(idx);
            unsigned ir = srcSigned(iv, R2);
            if (scale == 0) continue;
            if (llvm::isPowerOf2_64(scale)) {
              e(addLsl(d, acc, ir, unsigned(llvm::Log2_64(scale))));
            } else {
              movImm(R3, scale);
              e(mul(R3, ir, R3));
              e(addReg(d, acc, R3));
            }
            acc = d;
          }
        }
        if (constOff) {
          addConst(d, acc, constOff);
          acc = d;
        }
        if (acc != d) mv(d, acc);
        putRaw(d, g.getResult());
      })
      .Case<LL::ExtractValueOp>([&](auto x) {
        auto [off, t] = elementAt(x.getContainer().getType(), x.getPosition());
        if (auto lv = x.getContainer().template getDefiningOp<LL::LoadOp>(); lv && lazyLoad.count(lv)) {
          uint64_t ao;
          unsigned ar = addrReg(lv.getAddr(), R2, ao);
          if (isAggregate(t)) {
            copyBytes(SP, of(x.getResult()), ar, ao + off, std::max<uint64_t>((sizeOf(t) + 7) & ~uint64_t(7), 8));
            return;
          }
          unsigned d = dst(x.getResult(), R1);
          ldAt(d, ar, ao + off, sizeOf(t));
          putRaw(d, x.getResult());
          return;
        }
        uint64_t src = of(x.getContainer()) + off;
        if (isAggregate(t)) {
          copyBytes(SP, of(x.getResult()), SP, src, std::max<uint64_t>((sizeOf(t) + 7) & ~uint64_t(7), 8));
          return;
        }
        unsigned d = dst(x.getResult(), R1);
        ldAt(d, SP, src, sizeOf(t));
        putRaw(d, x.getResult());
      })
      .Case<LL::InsertValueOp>([&](auto ins) {
        if (lazyChainOps.count(ins.getOperation())) return;  // made at the call
        Type at = ins.getType();
        uint64_t sz = std::max<uint64_t>((sizeOf(at) + 7) & ~uint64_t(7), 8);
        if (of(ins.getResult()) != of(ins.getContainer()))  // else updated in place (layout)
          copyBytes(SP, of(ins.getResult()), SP, of(ins.getContainer()), sz);
        auto [off, t] = elementAt(at, ins.getPosition());
        uint64_t dst = of(ins.getResult()) + off;
        if (isAggregate(t)) {
          copyBytes(SP, dst, SP, of(ins.getValue()), sizeOf(t));
          return;
        }
        uint64_t bits;
        stAt(constBits(ins.getValue(), bits) && bits == 0 ? XZR : src(ins.getValue(), R1), SP, dst, sizeOf(t));
      })
      .Case<LL::CallOp>([&](auto c) { emitCall(c); })
      .Case<LL::MemcpyOp, LL::MemmoveOp>([&](auto m) {
        get(0, m.getDst());
        get(1, m.getSrc());
        get(2, m.getLen());
        uint64_t at = here();
        e(bl(0));
        w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol("_memmove"));
      })
      .Case<LL::MemsetOp>([&](auto m) {
        get(0, m.getDst());
        get(1, m.getVal());
        get(2, m.getLen());
        uint64_t at = here();
        e(bl(0));
        w.reloc(Sect::Text, at, Reloc::Branch26, w.symbol("_memset"));
      })
      .Case<LL::FMAOp, LL::FMulAddOp>([&](auto f) {
        bool dbl = f.getType().isF64();
        getF(F1, f.getA());
        getF(F2, f.getB());
        getF(F3, f.getC());
        e(fmadd(F1, F1, F2, F3, dbl));
        putF(F1, f.getResult());
      })
      .Case<LL::AtomicRMWOp>([&](auto a) {
        Type t = a.getVal().getType();
        unsigned ls = log2Size(sizeOf(t));
        get(R2, a.getPtr());
        get(R3, a.getVal());
        using B = LL::AtomicBinOp;
        switch (a.getBinOp()) {
        case B::add: e(ldaddal(R3, R1, R2, ls)); break;
        case B::sub:
          e(neg(R3, R3));
          e(ldaddal(R3, R1, R2, ls));
          break;
        case B::_and:
          e(mvn(R3, R3));
          e(ldclral(R3, R1, R2, ls));
          break;
        case B::_or: e(ldsetal(R3, R1, R2, ls)); break;
        case B::_xor: e(ldeoral(R3, R1, R2, ls)); break;
        case B::xchg: e(swpal(R3, R1, R2, ls)); break;
        default: unsupported("atomicrmw operation");
        }
        put(R1, a.getResult());
      })
      .Case<LL::AtomicCmpXchgOp>([&](auto x) {
        Type t = x.getVal().getType();
        unsigned ls = log2Size(sizeOf(t));
        get(R2, x.getPtr());
        get(R1, x.getCmp());
        mv(R4, R1);
        get(R3, x.getVal());
        e(casal(R1, R3, R2, ls));  // R1 <- old
        // Result {old, success}.
        uint64_t res = of(x.getResult());
        auto st = cast<LL::LLVMStructType>(x.getType());
        stAt(R1, SP, res + fieldOffset(st, 0), sizeOf(t));
        normalize(R1, t);
        normalize(R4, t);
        e(cmpReg(R1, R4));
        e(cset(R1, EQ));
        stAt(R1, SP, res + fieldOffset(st, 1), 1);
      })
      .Case<LL::FenceOp>([&](auto) { e(dmbIsh()); })
      .Case<LL::UnreachableOp>([&](auto) { e(brk(1)); })
      .Case<LL::ReturnOp>([&](auto r) { emitReturn(r); })
      .Case<LL::BrOp>([&](auto br_) {
        edgeMoves(br_.getDest(), br_.getDestOperands());
        if (br_.getDest() != nextBlock) branchTo(br_.getDest());
      })
      .Case<LL::CondBrOp>([&](auto c) {
        if (auto d = decidedBr.find(c.getOperation()); d != decidedBr.end()) {
          // A nil check of a pointer known to be non-nil here.
          if (d->second != nextBlock) branchTo(d->second);
          return;
        }
        bool flags = false, inv = false;  // inv: the condition holds when cr is zero
        Cond cc = AL;
        unsigned cr = 0;
        if (auto ic = c.getCondition().template getDefiningOp<LL::ICmpOp>(); ic && fusedCmp.count(ic)) {
          uint64_t bits;
          auto p = ic.getPredicate();
          if ((p == LL::ICmpPredicate::eq || p == LL::ICmpPredicate::ne) && constBits(ic.getRhs(), bits) &&
              bits == 0) {
            cr = src(ic.getLhs(), R1);  // cbz / cbnz
            inv = p == LL::ICmpPredicate::eq;
          } else {
            cmpFlags(ic);
            flags = true;
            cc = icmpCond(p);
          }
        } else if (auto tr = c.getCondition().template getDefiningOp<LL::TruncOp>(); tr && fusedCmp.count(tr)) {
          cr = src(tr.getArg(), R3);  // the bool byte itself
        } else {
          cr = src(c.getCondition(), R3);
        }
        Block *t = c.getTrueDest(), *f = c.getFalseDest();
        if (c.getTrueDestOperands().empty() && c.getFalseDestOperands().empty() && t == nextBlock) {
          jumpIf(flags, cc, cr, inv, f);  // falls through into the true block
        } else if (c.getTrueDestOperands().empty()) {
          jumpIf(flags, cc, cr, !inv, t);
          edgeMoves(f, c.getFalseDestOperands());
          if (f != nextBlock) branchTo(f);
        } else if (c.getFalseDestOperands().empty()) {
          jumpIf(flags, cc, cr, inv, f);
          edgeMoves(t, c.getTrueDestOperands());
          if (t != nextBlock) branchTo(t);
        } else {
          // Both edges move values: to the true edge's moves when the
          // condition holds, the false edge's moves fall through.
          uint64_t at = here();
          e(0);
          edgeMoves(f, c.getFalseDestOperands());
          branchTo(f);
          uint64_t trueAt = here();
          flushCache();
          int32_t off = int32_t(trueAt - at);
          uint32_t word = flags ? bcond(cc, off) : inv ? cbz(cr, off) : cbnz(cr, off);
          std::memcpy(&w.bytes(Sect::Text)[at], &word, 4);
          edgeMoves(t, c.getTrueDestOperands());
          if (t != nextBlock) branchTo(t);
        }
      })
      .Case<LL::SwitchOp>([&](auto s) {
        get(R3, s.getValue());
        auto cases = s.getCaseValues();
        if (!cases) {
          edgeMoves(s.getDefaultDestination(), s.getDefaultOperands());
          branchTo(s.getDefaultDestination());
          return;
        }
        unsigned i = 0;
        for (const APInt &v : cases->template getValues<APInt>()) {
          movImm(R4, v.getZExtValue() & (intWidth(s.getValue().getType()) == 64
                                             ? ~uint64_t(0)
                                             : ((uint64_t(1) << intWidth(s.getValue().getType())) - 1)));
          e(cmpReg(R3, R4));
          uint64_t at = here();
          e(bcond(NE, 0));
          edgeMoves(s.getCaseDestinations()[i], s.getCaseOperands(i));
          branchTo(s.getCaseDestinations()[i]);
          flushCache();  // the next case starts here
          uint32_t word = bcond(NE, int32_t(here() - at));
          std::memcpy(&w.bytes(Sect::Text)[at], &word, 4);
          i++;
        }
        edgeMoves(s.getDefaultDestination(), s.getDefaultOperands());
        branchTo(s.getDefaultDestination());
      })
      .Case<LL::CallIntrinsicOp>([&](auto c) { emitIntrinsic(c); })
      .Default([&](Operation *o) { unsupported("op " + o->getName().getStringRef().str()); });
}

// The intrinsics the front end emits as llvm.call_intrinsic. Integers are
// kept zero extended in 64 bits, so an iN operation runs on the 64-bit value
// and shifts the result into place.
void FuncGen::emitIntrinsic(LL::CallIntrinsicOp c) {
  StringRef name = c.getIntrin();
  auto args = c.getArgs();
  if (c.getNumResults() != 1) unsupported("intrinsic " + name.str());
  Value res = c.getResult(0);
  Type rt = res.getType();
  auto shiftDown = [&](unsigned r, unsigned n) {  // r >>= 64 - n
    if (n == 64) return;
    movImm(R3, 64 - n);
    e(lsrv(r, r, R3));
  };
  if (name.starts_with("llvm.ctlz.")) {
    unsigned n = intWidth(rt);
    get(R1, args[0]);
    e(clz(R1, R1));
    if (n < 64) e(subImm(R1, R1, 64 - n));
    put(R1, res);
  } else if (name.starts_with("llvm.cttz.")) {
    unsigned n = intWidth(rt);
    get(R1, args[0]);
    if (n < 64) {  // a zero input counts n
      movImm(R2, uint64_t(1) << n);
      e(orrReg(R1, R1, R2));
    }
    e(rbit(R1, R1));
    e(clz(R1, R1));
    put(R1, res);
  } else if (name.starts_with("llvm.ctpop.")) {
    get(R1, args[0]);
    e(fmovFromGp(F1, R1, true));
    e(cnt8b(F1, F1));
    e(addv8b(F1, F1));
    e(fmovToGp(R1, F1, true));
    put(R1, res);
  } else if (name.starts_with("llvm.bswap.")) {
    get(R1, args[0]);
    e(rev(R1, R1));
    shiftDown(R1, intWidth(rt));
    put(R1, res);
  } else if (name.starts_with("llvm.bitreverse.")) {
    get(R1, args[0]);
    e(rbit(R1, R1));
    shiftDown(R1, intWidth(rt));
    put(R1, res);
  } else if (name.starts_with("llvm.fshl.") || name.starts_with("llvm.fshr.")) {
    bool left = name.starts_with("llvm.fshl.");
    unsigned n = intWidth(rt);
    get(R1, args[0]);
    get(R2, args[1]);
    get(R3, args[2]);
    if (n == 64) {
      // fshl: a << s | (b >> 1) >> (63 - s); fshr: (a << 1) << (63 - s) | b >> s.
      movImm(R4, 63);
      e(andReg(R3, R3, R4));
      e(eorReg(R4, R3, R4));  // 63 - s
      if (left) {
        e(lslv(R1, R1, R3));
        movImm(RA, 1);
        e(lsrv(R2, R2, RA));
        e(lsrv(R2, R2, R4));
      } else {
        e(lsrv(R2, R2, R3));
        movImm(RA, 1);
        e(lslv(R1, R1, RA));
        e(lslv(R1, R1, R4));
      }
      e(orrReg(R1, R1, R2));
    } else if (n <= 32) {
      // Concatenate a:b in 64 bits, shift by s mod n, take the high or low n bits.
      movImm(R4, n);
      e(udiv(RA, R3, R4));
      e(msub(R3, RA, R4, R3));  // s mod n
      e(lslv(R1, R1, R4));
      e(orrReg(R1, R1, R2));
      if (left) {
        e(lslv(R1, R1, R3));
        e(lsrv(R1, R1, R4));
      } else {
        e(lsrv(R1, R1, R3));
      }
    } else {
      unsupported("intrinsic " + name.str());
    }
    put(R1, res);
  } else if (name == "llvm.fptosi.sat.i64.f64" || name == "llvm.fptoui.sat.i64.f64" ||
             name == "llvm.fptosi.sat.i64.f32" || name == "llvm.fptoui.sat.i64.f32") {
    // FCVTZS/FCVTZU saturate and turn NaN into 0, as the intrinsics require.
    bool dbl = args[0].getType().isF64();
    getF(F1, args[0]);
    e(name.starts_with("llvm.fptosi") ? fcvtzs(R1, F1, dbl) : fcvtzu(R1, F1, dbl));
    put(R1, res);
  } else if (name == "llvm.frameaddress.p0") {
    auto k = args[0].getDefiningOp<LL::ConstantOp>();
    auto ia = k ? dyn_cast<IntegerAttr>(k.getValue()) : IntegerAttr();
    if (!ia || ia.getInt() != 0) unsupported("frameaddress above level 0");
    e(movSp(R1, FP));
    put(R1, res);
  } else {
    unsupported("intrinsic " + name.str());
  }
}

void FuncGen::allocate() {
  static const unsigned regs[] = {19, 20, 21, 22, 23, 24, 25, 26, 27, 28};
  static const unsigned low[] = {0, 1, 2, 3, 4, 5, 6, 7};
  home = linearScan(fn, regs, fusedCmp, mg.returnsTwice, low);
  for (auto [v, r] : home)
    if (r >= 19) calleeUsed |= 1u << r;
}

void FuncGen::addressOf(unsigned d, LL::AddressOfOp a) {
  std::string name = machoName(a.getGlobalName());
  unsigned sym = w.symbol(name);
  uint64_t at = here();
  e(adrp(d));
  if (mg.isDefinedData(a.getGlobalName())) {
    w.reloc(Sect::Text, at, Reloc::Page21, sym);
    at = here();
    e(addImm(d, d, 0));
    w.reloc(Sect::Text, at, Reloc::PageOff12, sym);
  } else {
    w.reloc(Sect::Text, at, Reloc::GotPage21, sym);
    at = here();
    e(ldrImm(d, d, 0, 3));
    w.reloc(Sect::Text, at, Reloc::GotPageOff12, sym);
  }
}

void FuncGen::run() {
  if (fn.isVarArg()) unsupported("variadic function");
  mg.checkFunction(fn);
  analyze();
  allocate();
  layout();
  uint64_t start = here();
  fnStart = start;
  computeStackMaps();
  std::vector<ModuleGen::Row> rows;
  auto row = [&](Location l) {
    auto r = lineOf(l);
    if (!r) return;
    uint32_t file = mg.fileId(r->first);
    if (!rows.empty() && std::get<1>(rows.back()) == r->second && std::get<2>(rows.back()) == file) return;
    if (!rows.empty() && std::get<0>(rows.back()) == here() - start) rows.pop_back();
    rows.push_back({here() - start, r->second, file});
  };
  row(fn.getLoc());
  flushCache();
  std::map<std::string, uint64_t> ob;
  uint64_t was = here();
  prologue();
  if (opStatsOn) ob["prologue"] += here() - was;
  for (Block &b : fn.getBody()) {
    if (deadBlock.count(&b)) continue;
    blockStart[&b] = here();
    flushCache();
    nextBlock = b.getNextNode();
    while (nextBlock && deadBlock.count(nextBlock)) nextBlock = nextBlock->getNextNode();
    for (Operation &op : b) {
      row(op.getLoc());
      was = here();
      emitOp(op);
      if (opStatsOn) {
        bool agg = llvm::any_of(op.getResultTypes(), isAggregate) || llvm::any_of(op.getOperandTypes(), isAggregate);
        ob[op.getName().getStringRef().str() + (agg ? ".agg" : "")] += here() - was;
      }
    }
  }
  if (opStatsOn) {
    std::lock_guard<std::mutex> l(opBytesMu);
    for (auto &[k, v] : ob) opBytesAll[k] += v;
  }
  for (const Fixup &f : fixups) {
    int64_t off = int64_t(blockStart[f.dest]) - int64_t(f.at);
    if (f.kind != Fixup::B && (off >= (int64_t(1) << 20) || off < -(int64_t(1) << 20)))
      unsupported("conditional branch out of range");
    uint32_t word = f.kind == Fixup::B       ? b(int32_t(off))
                    : f.kind == Fixup::BCond ? bcond(f.cond, int32_t(off))
                    : f.kind == Fixup::Cbnz  ? cbnz(f.reg, int32_t(off))
                                             : cbz(f.reg, int32_t(off));
    std::memcpy(&w.bytes(Sect::Text)[f.at], &word, 4);
  }
  // Defined only now: a function that falls back leaves no symbol.
  auto lk = fn.getLinkage();
  std::string sym = machoName(fn.getSymName());
  w.define(sym, Sect::Text, start, !isLocalLinkage(lk), isWeakLinkage(lk), isHidden(fn));
  mg.lines.push_back({w.symbol(sym), here() - start, std::move(rows), std::move(smaps), std::move(smSites)});
}

} // namespace

void runArm64(ModuleGen &mg, LL::LLVMFuncOp f) { FuncGen(mg, f).run(); }

} // namespace tier0
} // namespace marmot

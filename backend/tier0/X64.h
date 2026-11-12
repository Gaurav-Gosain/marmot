// X64.h: x86-64 instruction encoders for tier0 (x86-64-v2: SSE4.2, POPCNT;
// no BMI, LZCNT or FMA). Each appends to a byte buffer. Memory operands are
// [base + disp32]; general registers 0-15 (rax..r15), xmm 0-15.
#ifndef MARMOT_TIER0_X64_H
#define MARMOT_TIER0_X64_H

#include <cstdint>
#include <vector>

namespace marmot {
namespace tier0 {
namespace x64 {

enum : unsigned { RAX = 0, RCX, RDX, RBX, RSP, RBP, RSI, RDI, R8, R9, R10, R11, R12, R13, R14, R15 };

// Condition codes (the low nibble of Jcc / SETcc / CMOVcc).
enum CC : uint8_t { O = 0, NO, B, AE, E, NE, BE, A, S, NS, P, NP, L, GE, LE, G };

class Asm {
public:
  explicit Asm(std::vector<uint8_t> &out) : b(out) {}
  size_t here() const { return b.size(); }

  // --- encoding helpers ---
  void u8(uint8_t v) { b.push_back(v); }
  void u32(uint32_t v) {
    for (int i = 0; i < 4; i++) b.push_back(uint8_t(v >> (8 * i)));
  }
  void u64(uint64_t v) {
    for (int i = 0; i < 8; i++) b.push_back(uint8_t(v >> (8 * i)));
  }
  // REX prefix: w (64-bit), r (reg high bit), x, b (rm/base high bit).
  // force: emit even when empty (byte registers sil/dil/spl/bpl).
  void rex(bool w, unsigned reg, unsigned rm, bool force = false) {
    uint8_t r = uint8_t(0x40 | (w ? 8 : 0) | ((reg >> 3) & 1) << 2 | ((rm >> 3) & 1));
    if (r != 0x40 || force) u8(r);
  }
  void modrmReg(unsigned reg, unsigned rm) { u8(uint8_t(0xC0 | (reg & 7) << 3 | (rm & 7))); }
  void modrmMem(unsigned reg, unsigned base, int32_t disp) {
    u8(uint8_t(0x80 | (reg & 7) << 3 | (base & 7)));
    if ((base & 7) == RSP) u8(0x24);  // SIB: no index, base rsp/r12
    u32(uint32_t(disp));
  }
  // op r, r/m (register form): opcode bytes then modrm.
  void rr(bool w, std::initializer_list<uint8_t> op, unsigned reg, unsigned rm, bool byteRegs = false) {
    rex(w, reg, rm, byteRegs && (reg >= 4 || rm >= 4));
    for (uint8_t o : op) u8(o);
    modrmReg(reg, rm);
  }
  void rm(bool w, std::initializer_list<uint8_t> op, unsigned reg, unsigned base, int32_t disp,
          bool byteReg = false) {
    rex(w, reg, base, byteReg && reg >= 4);
    for (uint8_t o : op) u8(o);
    modrmMem(reg, base, disp);
  }
  // SSE: prefix, REX, 0F op.
  void sse(uint8_t prefix, bool w, uint8_t op, unsigned reg, unsigned rmReg) {
    if (prefix) u8(prefix);
    rex(w, reg, rmReg);
    u8(0x0F);
    u8(op);
    modrmReg(reg, rmReg);
  }

  // --- moves ---
  void mov(unsigned d, unsigned s) { rr(true, {0x89}, s, d); }           // mov d, s
  void mov32(unsigned d, unsigned s) { rr(false, {0x89}, s, d); }        // zero extends
  void movImm(unsigned d, uint64_t v) {
    if (v <= 0xFFFFFFFFu) {  // mov r32, imm32 (zero extends)
      rex(false, 0, d);
      u8(uint8_t(0xB8 | (d & 7)));
      u32(uint32_t(v));
    } else if (int64_t(v) >= INT32_MIN && int64_t(v) < 0) {  // mov r64, simm32
      rex(true, 0, d);
      u8(0xC7);
      modrmReg(0, d);
      u32(uint32_t(v));
    } else {
      rex(true, 0, d);
      u8(uint8_t(0xB8 | (d & 7)));
      u64(v);
    }
  }
  // Loads zero extended to 64 bits, of 1, 2, 4 or 8 bytes.
  void load(unsigned d, unsigned base, int32_t disp, unsigned size) {
    switch (size) {
    case 1: rm(false, {0x0F, 0xB6}, d, base, disp); break;  // movzx r32, m8
    case 2: rm(false, {0x0F, 0xB7}, d, base, disp); break;  // movzx r32, m16
    case 4: rm(false, {0x8B}, d, base, disp); break;
    default: rm(true, {0x8B}, d, base, disp); break;
    }
  }
  void loadSigned(unsigned d, unsigned base, int32_t disp, unsigned size) {
    switch (size) {
    case 1: rm(true, {0x0F, 0xBE}, d, base, disp); break;
    case 2: rm(true, {0x0F, 0xBF}, d, base, disp); break;
    case 4: rm(true, {0x63}, d, base, disp); break;  // movsxd
    default: rm(true, {0x8B}, d, base, disp); break;
    }
  }
  void store(unsigned s, unsigned base, int32_t disp, unsigned size) {
    switch (size) {
    case 1: rm(false, {0x88}, s, base, disp, true); break;
    case 2: u8(0x66); rm(false, {0x89}, s, base, disp); break;
    case 4: rm(false, {0x89}, s, base, disp); break;
    default: rm(true, {0x89}, s, base, disp); break;
    }
  }
  void lea(unsigned d, unsigned base, int32_t disp) { rm(true, {0x8D}, d, base, disp); }
  // lea d, [rip + disp32]: returns the offset of disp32 (for a relocation).
  size_t leaRip(unsigned d) {
    rex(true, d, 0);
    u8(0x8D);
    u8(uint8_t(0x05 | (d & 7) << 3));
    size_t at = here();
    u32(0);
    return at;
  }
  // mov d, [rip + disp32] (a GOT load): returns the offset of disp32.
  size_t loadRip(unsigned d) {
    rex(true, d, 0);
    u8(0x8B);
    u8(uint8_t(0x05 | (d & 7) << 3));
    size_t at = here();
    u32(0);
    return at;
  }
  // Zero / sign extension in a register.
  void movzx8(unsigned d, unsigned s) { rr(false, {0x0F, 0xB6}, d, s, true); }
  void movzx16(unsigned d, unsigned s) { rr(false, {0x0F, 0xB7}, d, s); }
  void movsx8(unsigned d, unsigned s) { rr(true, {0x0F, 0xBE}, d, s, true); }
  void movsx16(unsigned d, unsigned s) { rr(true, {0x0F, 0xBF}, d, s); }
  void movsx32(unsigned d, unsigned s) { rr(true, {0x63}, d, s); }

  // --- integer arithmetic (64-bit unless noted): d = d op s ---
  void add(unsigned d, unsigned s) { rr(true, {0x01}, s, d); }
  void sub(unsigned d, unsigned s) { rr(true, {0x29}, s, d); }
  void and_(unsigned d, unsigned s) { rr(true, {0x21}, s, d); }
  void or_(unsigned d, unsigned s) { rr(true, {0x09}, s, d); }
  void xor_(unsigned d, unsigned s) { rr(true, {0x31}, s, d); }
  void cmp(unsigned a, unsigned c) { rr(true, {0x39}, c, a); }  // flags of a - c
  void test(unsigned a, unsigned c) { rr(true, {0x85}, c, a); }
  void adc(unsigned d, unsigned s) { rr(true, {0x11}, s, d); }
  void sbb(unsigned d, unsigned s) { rr(true, {0x19}, s, d); }
  void imul(unsigned d, unsigned s) { rr(true, {0x0F, 0xAF}, d, s); }
  void neg(unsigned d) { rr(true, {0xF7}, 3, d); }
  void not_(unsigned d) { rr(true, {0xF7}, 2, d); }
  void mulRdx(unsigned s) { rr(true, {0xF7}, 4, s); }  // rdx:rax = rax * s (unsigned)
  void div(unsigned s) { rr(true, {0xF7}, 6, s); }     // rdx:rax / s
  void idiv(unsigned s) { rr(true, {0xF7}, 7, s); }
  void cqo() { u8(0x48); u8(0x99); }
  // Shifts by cl: kind 4 shl, 5 shr, 7 sar.
  void shiftCl(unsigned kind, unsigned d) { rr(true, {0xD3}, kind, d); }
  void shiftImm(unsigned kind, unsigned d, uint8_t n) {
    rr(true, {0xC1}, kind, d);
    u8(n);
  }
  void addImm(unsigned d, int32_t v) {  // add d, simm32
    rr(true, {0x81}, 0, d);
    u32(uint32_t(v));
  }
  void subImm(unsigned d, int32_t v) {
    rr(true, {0x81}, 5, d);
    u32(uint32_t(v));
  }
  void cmpImm(unsigned d, int32_t v) {
    rr(true, {0x81}, 7, d);
    u32(uint32_t(v));
  }
  void setcc(CC c, unsigned d) { rr(false, {0x0F, uint8_t(0x90 | c)}, 0, d, true); }
  void cmov(CC c, unsigned d, unsigned s) { rr(true, {0x0F, uint8_t(0x40 | c)}, d, s); }
  void bsr(unsigned d, unsigned s) { rr(true, {0x0F, 0xBD}, d, s); }
  void bsf(unsigned d, unsigned s) { rr(true, {0x0F, 0xBC}, d, s); }
  void popcnt(unsigned d, unsigned s) {
    u8(0xF3);
    rr(true, {0x0F, 0xB8}, d, s);
  }
  void bswap(unsigned d) {
    rex(true, 0, d);
    u8(0x0F);
    u8(uint8_t(0xC8 | (d & 7)));
  }
  // shld d, s, cl / shrd d, s, cl.
  void shldCl(unsigned d, unsigned s) { rr(true, {0x0F, 0xA5}, s, d); }
  void shrdCl(unsigned d, unsigned s) { rr(true, {0x0F, 0xAD}, s, d); }

  // --- control flow; the rel32 helpers return the offset of the field ---
  size_t call() {
    u8(0xE8);
    size_t at = here();
    u32(0);
    return at;
  }
  void callReg(unsigned r) { rr(false, {0xFF}, 2, r); }
  size_t jmp() {
    u8(0xE9);
    size_t at = here();
    u32(0);
    return at;
  }
  size_t jcc(CC c) {
    u8(0x0F);
    u8(uint8_t(0x80 | c));
    size_t at = here();
    u32(0);
    return at;
  }
  void ret() { u8(0xC3); }
  void push(unsigned r) {
    rex(false, 0, r);
    u8(uint8_t(0x50 | (r & 7)));
  }
  void pop(unsigned r) {
    rex(false, 0, r);
    u8(uint8_t(0x58 | (r & 7)));
  }
  void ud2() { u8(0x0F); u8(0x0B); }
  // x87: fld / fstp of a 4 or 8 byte float in memory.
  void fld(unsigned base, int32_t disp, bool dbl) { rm(false, {uint8_t(dbl ? 0xDD : 0xD9)}, 0, base, disp); }
  void fstp(unsigned base, int32_t disp, bool dbl) { rm(false, {uint8_t(dbl ? 0xDD : 0xD9)}, 3, base, disp); }
  void mfence() { u8(0x0F); u8(0xAE); u8(0xF0); }

  // --- atomics: lock prefixed, [base + 0] ---
  void lockXadd(unsigned s, unsigned base, unsigned size) { lockOp({0x0F, size == 1 ? uint8_t(0xC0) : uint8_t(0xC1)}, s, base, size); }
  void xchg(unsigned s, unsigned base, unsigned size) { plainOp({size == 1 ? uint8_t(0x86) : uint8_t(0x87)}, s, base, size); }
  void lockCmpxchg(unsigned s, unsigned base, unsigned size) { lockOp({0x0F, size == 1 ? uint8_t(0xB0) : uint8_t(0xB1)}, s, base, size); }

  // --- SSE scalar (dbl: sd, else ss) ---
  void movqToX(unsigned x, unsigned r) { u8(0x66); rex(true, x, r); u8(0x0F); u8(0x6E); modrmReg(x, r); }
  void movqFromX(unsigned r, unsigned x) { u8(0x66); rex(true, x, r); u8(0x0F); u8(0x7E); modrmReg(x, r); }
  void fop(uint8_t op, unsigned d, unsigned s, bool dbl) { sse(dbl ? 0xF2 : 0xF3, false, op, d, s); }
  void addsd(unsigned d, unsigned s, bool dbl) { fop(0x58, d, s, dbl); }
  void mulsd(unsigned d, unsigned s, bool dbl) { fop(0x59, d, s, dbl); }
  void subsd(unsigned d, unsigned s, bool dbl) { fop(0x5C, d, s, dbl); }
  void divsd(unsigned d, unsigned s, bool dbl) { fop(0x5E, d, s, dbl); }
  void sqrtsd(unsigned d, unsigned s, bool dbl) { fop(0x51, d, s, dbl); }
  void minsd(unsigned d, unsigned s, bool dbl) { fop(0x5D, d, s, dbl); }
  void maxsd(unsigned d, unsigned s, bool dbl) { fop(0x5F, d, s, dbl); }
  void ucomi(unsigned a, unsigned c, bool dbl) { sse(dbl ? 0x66 : 0, false, 0x2E, a, c); }
  void cvtsd2ss(unsigned d, unsigned s) { fop(0x5A, d, s, true); }
  void cvtss2sd(unsigned d, unsigned s) { fop(0x5A, d, s, false); }
  void cvtsi2f(unsigned x, unsigned r, bool dbl) { sse(dbl ? 0xF2 : 0xF3, true, 0x2A, x, r); }    // from int64
  void cvttf2si(unsigned r, unsigned x, bool dbl) { sse(dbl ? 0xF2 : 0xF3, true, 0x2C, r, x); }   // to int64

private:
  std::vector<uint8_t> &b;
  void lockOp(std::initializer_list<uint8_t> op, unsigned s, unsigned base, unsigned size) {
    u8(0xF0);
    plainOp(op, s, base, size);
  }
  void plainOp(std::initializer_list<uint8_t> op, unsigned s, unsigned base, unsigned size) {
    if (size == 2) u8(0x66);
    rm(size == 8, op, s, base, 0, size == 1);
  }
};

} // namespace x64
} // namespace tier0
} // namespace marmot

#endif

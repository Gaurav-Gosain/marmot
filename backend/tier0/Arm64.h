// Arm64.h: encoders for the arm64 instructions tier0 emits. Each function
// returns one 32-bit instruction word. Registers are numbers 0..31; 31 is
// sp or xzr depending on the instruction, as in the architecture.
#ifndef MARMOT_TIER0_ARM64_H
#define MARMOT_TIER0_ARM64_H

#include <cstdint>

namespace marmot::tier0::a64 {

enum : unsigned { X0 = 0, X8 = 8, X16 = 16, X17 = 17, FP = 29, LR = 30, SP = 31, XZR = 31 };

// Condition codes.
enum Cond : unsigned { EQ = 0, NE, HS, LO, MI, PL, VS, VC, HI, LS, GE, LT, GT, LE, AL };

inline uint32_t sf(bool x64) { return x64 ? 1u << 31 : 0; }

// ADD/SUB (shifted register): rd = rn op rm.
inline uint32_t addReg(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x0B000000u | (rm << 16) | (rn << 5) | rd;
}
inline uint32_t subReg(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x4B000000u | (rm << 16) | (rn << 5) | rd;
}
// SUBS with zero destination: compare.
inline uint32_t cmpReg(unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x6B000000u | (rm << 16) | (rn << 5) | XZR;
}
// ADD/SUB immediate (12 bits, optional << 12). Register 31 is sp here.
inline uint32_t addImm(unsigned rd, unsigned rn, uint32_t imm12, bool x64 = true, bool shift12 = false) {
  return sf(x64) | 0x11000000u | (shift12 ? 1u << 22 : 0) | ((imm12 & 0xFFF) << 10) | (rn << 5) | rd;
}
inline uint32_t subImm(unsigned rd, unsigned rn, uint32_t imm12, bool x64 = true, bool shift12 = false) {
  return sf(x64) | 0x51000000u | (shift12 ? 1u << 22 : 0) | ((imm12 & 0xFFF) << 10) | (rn << 5) | rd;
}
inline uint32_t cmpImm(unsigned rn, uint32_t imm12, bool x64 = true) {
  return sf(x64) | 0x71000000u | ((imm12 & 0xFFF) << 10) | (rn << 5) | XZR;
}
// Logical (shifted register).
inline uint32_t andReg(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x0A000000u | (rm << 16) | (rn << 5) | rd;
}
inline uint32_t orrReg(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x2A000000u | (rm << 16) | (rn << 5) | rd;
}
inline uint32_t eorReg(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x4A000000u | (rm << 16) | (rn << 5) | rd;
}
// MOV register (ORR with xzr). Not for sp.
inline uint32_t movReg(unsigned rd, unsigned rm, bool x64 = true) { return orrReg(rd, XZR, rm, x64); }
// MOV to or from sp (ADD #0).
inline uint32_t movSp(unsigned rd, unsigned rn) { return addImm(rd, rn, 0); }
// Multiply and divide.
inline uint32_t mul(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x1B007C00u | (rm << 16) | (rn << 5) | rd;  // MADD with xzr
}
inline uint32_t msub(unsigned rd, unsigned rn, unsigned rm, unsigned ra, bool x64 = true) {
  return sf(x64) | 0x1B008000u | (rm << 16) | (ra << 10) | (rn << 5) | rd;
}
inline uint32_t sdiv(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x1AC00C00u | (rm << 16) | (rn << 5) | rd;
}
inline uint32_t udiv(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x1AC00800u | (rm << 16) | (rn << 5) | rd;
}
// Variable shifts.
inline uint32_t lslv(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x1AC02000u | (rm << 16) | (rn << 5) | rd;
}
inline uint32_t lsrv(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x1AC02400u | (rm << 16) | (rn << 5) | rd;
}
inline uint32_t asrv(unsigned rd, unsigned rn, unsigned rm, bool x64 = true) {
  return sf(x64) | 0x1AC02800u | (rm << 16) | (rn << 5) | rd;
}
// CSET rd, cond (CSINC rd, xzr, xzr, invert(cond)).
inline uint32_t cset(unsigned rd, Cond c, bool x64 = true) {
  return sf(x64) | 0x1A9F07E0u | ((unsigned(c) ^ 1u) << 12) | rd;
}
// CSEL rd, rn, rm, cond.
inline uint32_t csel(unsigned rd, unsigned rn, unsigned rm, Cond c, bool x64 = true) {
  return sf(x64) | 0x1A800000u | (rm << 16) | (unsigned(c) << 12) | (rn << 5) | rd;
}
// MOVZ/MOVK/MOVN: 16-bit chunk at hw*16.
inline uint32_t movz(unsigned rd, uint32_t imm16, unsigned hw, bool x64 = true) {
  return sf(x64) | 0x52800000u | (hw << 21) | ((imm16 & 0xFFFF) << 5) | rd;
}
inline uint32_t movk(unsigned rd, uint32_t imm16, unsigned hw, bool x64 = true) {
  return sf(x64) | 0x72800000u | (hw << 21) | ((imm16 & 0xFFFF) << 5) | rd;
}
inline uint32_t movn(unsigned rd, uint32_t imm16, unsigned hw, bool x64 = true) {
  return sf(x64) | 0x12800000u | (hw << 21) | ((imm16 & 0xFFFF) << 5) | rd;
}
// Sign and zero extension (SBFM/UBFM aliases).
inline uint32_t sxtb(unsigned rd, unsigned rn) { return 0x93401C00u | (rn << 5) | rd; }
inline uint32_t sxth(unsigned rd, unsigned rn) { return 0x93403C00u | (rn << 5) | rd; }
inline uint32_t sxtw(unsigned rd, unsigned rn) { return 0x93407C00u | (rn << 5) | rd; }
inline uint32_t uxtb(unsigned rd, unsigned rn) { return 0x53001C00u | (rn << 5) | rd; }  // 32-bit, zero extends
inline uint32_t uxth(unsigned rd, unsigned rn) { return 0x53003C00u | (rn << 5) | rd; }
inline uint32_t uxtw(unsigned rd, unsigned rn) { return movReg(rd, rn, false); }  // MOV w: clears the top half

// Loads and stores, unsigned scaled 12-bit offset (bytes = imm << size).
// size: 0 byte, 1 half, 2 word, 3 double word.
inline uint32_t ldrImm(unsigned rt, unsigned rn, uint32_t off, unsigned size) {
  return (size << 30) | 0x39400000u | (((off >> size) & 0xFFF) << 10) | (rn << 5) | rt;
}
inline uint32_t strImm(unsigned rt, unsigned rn, uint32_t off, unsigned size) {
  return (size << 30) | 0x39000000u | (((off >> size) & 0xFFF) << 10) | (rn << 5) | rt;
}
// Signed loads (to 64 bits): LDRSB/LDRSH/LDRSW.
inline uint32_t ldrsImm(unsigned rt, unsigned rn, uint32_t off, unsigned size) {
  return (size << 30) | 0x39800000u | (((off >> size) & 0xFFF) << 10) | (rn << 5) | rt;
}
// Unscaled 9-bit signed offset (LDUR/STUR).
inline uint32_t ldur(unsigned rt, unsigned rn, int32_t off, unsigned size) {
  return (size << 30) | 0x38400000u | ((uint32_t(off) & 0x1FF) << 12) | (rn << 5) | rt;
}
inline uint32_t stur(unsigned rt, unsigned rn, int32_t off, unsigned size) {
  return (size << 30) | 0x38000000u | ((uint32_t(off) & 0x1FF) << 12) | (rn << 5) | rt;
}
// Register offset: [rn, rm].
inline uint32_t ldrReg(unsigned rt, unsigned rn, unsigned rm, unsigned size) {
  return (size << 30) | 0x38606800u | (rm << 16) | (rn << 5) | rt;
}
inline uint32_t strReg(unsigned rt, unsigned rn, unsigned rm, unsigned size) {
  return (size << 30) | 0x38206800u | (rm << 16) | (rn << 5) | rt;
}
// STP/LDP of x registers, pre-index and post-index (offset in bytes, multiple of 8).
inline uint32_t stpPre(unsigned rt, unsigned rt2, unsigned rn, int32_t off) {
  return 0xA9800000u | ((uint32_t(off / 8) & 0x7F) << 15) | (rt2 << 10) | (rn << 5) | rt;
}
inline uint32_t ldpPost(unsigned rt, unsigned rt2, unsigned rn, int32_t off) {
  return 0xA8C00000u | ((uint32_t(off / 8) & 0x7F) << 15) | (rt2 << 10) | (rn << 5) | rt;
}
inline uint32_t stpOff(unsigned rt, unsigned rt2, unsigned rn, int32_t off) {
  return 0xA9000000u | ((uint32_t(off / 8) & 0x7F) << 15) | (rt2 << 10) | (rn << 5) | rt;
}
inline uint32_t ldpOff(unsigned rt, unsigned rt2, unsigned rn, int32_t off) {
  return 0xA9400000u | ((uint32_t(off / 8) & 0x7F) << 15) | (rt2 << 10) | (rn << 5) | rt;
}

// Branches. Offsets in bytes from this instruction.
inline uint32_t b(int32_t off) { return 0x14000000u | ((uint32_t(off) >> 2) & 0x3FFFFFF); }
inline uint32_t bl(int32_t off) { return 0x94000000u | ((uint32_t(off) >> 2) & 0x3FFFFFF); }
inline uint32_t bcond(Cond c, int32_t off) { return 0x54000000u | (((uint32_t(off) >> 2) & 0x7FFFF) << 5) | c; }
inline uint32_t cbz(unsigned rt, int32_t off, bool x64 = true) {
  return sf(x64) | 0x34000000u | (((uint32_t(off) >> 2) & 0x7FFFF) << 5) | rt;
}
inline uint32_t cbnz(unsigned rt, int32_t off, bool x64 = true) {
  return sf(x64) | 0x35000000u | (((uint32_t(off) >> 2) & 0x7FFFF) << 5) | rt;
}
inline uint32_t br(unsigned rn) { return 0xD61F0000u | (rn << 5); }
inline uint32_t blr(unsigned rn) { return 0xD63F0000u | (rn << 5); }
inline uint32_t ret() { return 0xD65F03C0u; }
inline uint32_t brk(uint32_t imm16) { return 0xD4200000u | ((imm16 & 0xFFFF) << 5); }
// ADRP rd, page (immediate set by a relocation); ADD/LDR use PAGEOFF12.
inline uint32_t adrp(unsigned rd) { return 0x90000000u | rd; }
inline uint32_t nop() { return 0xD503201Fu; }

} // namespace marmot::tier0::a64

#endif

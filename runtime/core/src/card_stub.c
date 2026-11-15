// Card stubs for compiled code: rt_card_xN (arm64) and rt_card_rN (x86-64)
// clear the card of the address in register N when it is not clear yet,
// the same conditional store as rt.h describes. Compiled code calls one
// after a pointer store, while rt_gc_marking is set:
//
//   arm64:  ldrb w16, rt_gc_marking; cbz w16, 1f; bl rt_card_xN; 1:
//   x86-64: cmpb $0, rt_gc_marking(%rip); je 1f; call rt_card_rN; 1:
//
// rt_cardt_xN and rt_cardt_rN test rt_gc_marking themselves, so a site is
// one call (bl rt_cardt_xN) at the cost of a call per store when not
// marking; they keep the same registers as the others.
//
// Contract: the stub reads register N and preserves every general register
// and the stack, except that on arm64 it clobbers x16 and x17 (the
// intra-procedure-call scratch registers, which a linker veneer may
// clobber at any bl anyway) and the condition flags, and on x86-64 it
// clobbers the flags only. It needs no stack alignment and calls nothing.
// There is a stub for every register that can hold an address: arm64
// x0-x15 and x19-x28 (not x16-x18, the frame pointer or the link register),
// x86-64 every general register but rsp.
#include "internal.h"

_Static_assert(RT_CARD_SHIFT == 9 && RT_CARD_BITS == 27, "card_stub.c hard-codes the card index");

#if defined(__APPLE__)
#define CS_SYM(n) "_" n
#define CS_TYPE(n) ""
#define CS_PRIV(n) ".private_extern " CS_SYM(n) "\n"
#else
#define CS_SYM(n) n
#define CS_TYPE(n) ".type " n ", @function\n"
#define CS_PRIV(n) ".hidden " n "\n"
#endif

#if RT_ARCH_ARM64 && !defined(__wasm__)

#if defined(__APPLE__)
#define CS_LOADTAB                                    \
  "adrp x16, " CS_SYM("rt_card_table") "@PAGE\n"      \
  "ldr x16, [x16, " CS_SYM("rt_card_table") "@PAGEOFF]\n"
#define CS_LOADFLAG                                   \
  "adrp x16, " CS_SYM("rt_gc_marking") "@PAGE\n"      \
  "ldrb w16, [x16, " CS_SYM("rt_gc_marking") "@PAGEOFF]\n"
#else
#define CS_LOADTAB                    \
  "adrp x16, rt_card_table\n"         \
  "ldr x16, [x16, :lo12:rt_card_table]\n"
#define CS_LOADFLAG                   \
  "adrp x16, rt_gc_marking\n"         \
  "ldrb w16, [x16, :lo12:rt_gc_marking]\n"
#endif

// rt_card_xN: x16 = the card of xN (table + index in x17); clear it if it
// is set. rt_cardt_xN: the same after returning unless marking. Each stub
// is whole (no fall-through into another symbol): the Mach-O linker drops
// a symbol's code when nothing references it.
#define CS_ARM64_BODY                                            \
  "add x16, x16, x17\n"                                          \
  "ldrb w17, [x16]\n"                                            \
  "cbz w17, 1f\n"                                                \
  "strb wzr, [x16]\n"                                            \
  "1: ret\n"
#define CS_ARM64(r)                                              \
  __asm__(".text\n.p2align 4\n"                                  \
          ".globl " CS_SYM("rt_card_" #r) "\n" CS_PRIV("rt_card_" #r) \
          CS_TYPE("rt_card_" #r) CS_SYM("rt_card_" #r) ":\n"     \
          "ubfx x17, " #r ", #9, #27\n" CS_LOADTAB CS_ARM64_BODY \
          ".p2align 4\n"                                          \
          ".globl " CS_SYM("rt_cardt_" #r) "\n" CS_PRIV("rt_cardt_" #r) \
          CS_TYPE("rt_cardt_" #r) CS_SYM("rt_cardt_" #r) ":\n"   \
          CS_LOADFLAG                                            \
          "cbz w16, 1f\n"                                        \
          "ubfx x17, " #r ", #9, #27\n" CS_LOADTAB CS_ARM64_BODY);

CS_ARM64(x0)
CS_ARM64(x1)
CS_ARM64(x2)
CS_ARM64(x3)
CS_ARM64(x4)
CS_ARM64(x5)
CS_ARM64(x6)
CS_ARM64(x7)
CS_ARM64(x8)
CS_ARM64(x9)
CS_ARM64(x10)
CS_ARM64(x11)
CS_ARM64(x12)
CS_ARM64(x13)
CS_ARM64(x14)
CS_ARM64(x15)
CS_ARM64(x19)
CS_ARM64(x20)
CS_ARM64(x21)
CS_ARM64(x22)
CS_ARM64(x23)
CS_ARM64(x24)
CS_ARM64(x25)
CS_ARM64(x26)
CS_ARM64(x27)
CS_ARM64(x28)

#elif RT_ARCH_AMD64

// s (a register other than r) is saved, then holds the card's address.
#define CS_AMD64_BODY(r, s)                                                  \
  "push %" #s "\n"                                                           \
  "mov %" #r ", %" #s "\n"                                                   \
  "shr $9, %" #s "\n"                                                        \
  "and $0x7ffffff, %" #s "\n"                                                \
  "add " CS_SYM("rt_card_table") "(%rip), %" #s "\n"                         \
  "cmpb $0, (%" #s ")\n"                                                     \
  "je 1f\n"                                                                  \
  "movb $0, (%" #s ")\n"                                                     \
  "1: pop %" #s "\n"                                                         \
  "2: ret\n"
#define CS_AMD64(r, s)                                                       \
  __asm__(".text\n.p2align 4\n"                                              \
          ".globl " CS_SYM("rt_card_" #r) "\n" CS_PRIV("rt_card_" #r)        \
          CS_TYPE("rt_card_" #r) CS_SYM("rt_card_" #r) ":\n"                 \
          CS_AMD64_BODY(r, s)                                                \
          ".p2align 4\n"                                                     \
          ".globl " CS_SYM("rt_cardt_" #r) "\n" CS_PRIV("rt_cardt_" #r)      \
          CS_TYPE("rt_cardt_" #r) CS_SYM("rt_cardt_" #r) ":\n"               \
          "cmpb $0, " CS_SYM("rt_gc_marking") "(%rip)\n"                     \
          "je 3f\n"                                                          \
          CS_AMD64_BODY(r, s)                                                \
          "3: ret\n");

CS_AMD64(rax, rcx)
CS_AMD64(rcx, rax)
CS_AMD64(rdx, rax)
CS_AMD64(rbx, rax)
CS_AMD64(rsi, rax)
CS_AMD64(rdi, rax)
CS_AMD64(rbp, rax)
CS_AMD64(r8, rax)
CS_AMD64(r9, rax)
CS_AMD64(r10, rax)
CS_AMD64(r11, rax)
CS_AMD64(r12, rax)
CS_AMD64(r13, rax)
CS_AMD64(r14, rax)
CS_AMD64(r15, rax)

#endif

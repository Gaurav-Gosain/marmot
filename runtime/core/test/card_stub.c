// The per-register card stubs (card_stub.c): each clears the card of the
// address in its register when the card is set, leaves a clear card alone,
// and keeps its own register and the other registers (here one caller-saved
// and one callee-saved register around each call, and a SIMD register).
// rt_cardt_* do the same only while rt_gc_marking is set.
// expect-stdout: ok
// wasm: skip (no stubs: wasm uses rt_wb)
#include "rtest.h"

static uint64_t word[64];

static uint8_t *card_of(void *p) {
  return &rt_card_table[((uintptr_t)p >> RT_CARD_SHIFT) & RT_CARD_MASK];
}

#if RT_ARCH_ARM64
#if defined(__APPLE__)
#define STUB(r) "bl _rt_card_" #r
#else
#define STUB(r) "bl rt_card_" #r
#endif
// a in register r, sentinels in o1 (caller-saved) and o2 (callee-saved).
#define T(r, o1, o2)                                                             \
  static void t_##r(void *p) {                                                   \
    register uintptr_t a __asm__(#r) = (uintptr_t)p;                             \
    register uintptr_t s1 __asm__(#o1) = 0x1111;                                 \
    register uintptr_t s2 __asm__(#o2) = 0x2222;                                 \
    uintptr_t a0 = a;                                                            \
    double d = 3.5;                                                              \
    __asm__ volatile("fmov d8, %3\n" STUB(r) "\nfmov %3, d8"                     \
                     : "+r"(a), "+r"(s1), "+r"(s2), "+w"(d)                      \
                     :                                                           \
                     : "x16", "x17", "x30", "cc", "memory", "d8");               \
    CHECK(a == a0 && s1 == 0x1111 && s2 == 0x2222 && d == 3.5);                  \
  }
T(x0, x1, x19)
T(x7, x9, x20)
T(x15, x14, x28)
T(x19, x0, x21)
T(x28, x15, x19)
#define NREG 5
static void (*const fns[])(void *) = {t_x0, t_x7, t_x15, t_x19, t_x28};
#if defined(__APPLE__)
#define TSTUB(r) "bl _rt_cardt_" #r
#else
#define TSTUB(r) "bl rt_cardt_" #r
#endif
static void tt(void *p) {
  register uintptr_t a __asm__("x9") = (uintptr_t)p;
  register uintptr_t s1 __asm__("x10") = 0x1111;
  register uintptr_t s2 __asm__("x22") = 0x2222;
  uintptr_t a0 = a;
  __asm__ volatile(TSTUB(x9) : "+r"(a), "+r"(s1), "+r"(s2) : : "x16", "x17", "x30", "cc", "memory");
  CHECK(a == a0 && s1 == 0x1111 && s2 == 0x2222);
}
#elif RT_ARCH_AMD64
#if defined(__APPLE__)
#define STUB(r) "call _rt_card_" #r
#else
#define STUB(r) "call rt_card_" #r
#endif
#define T(r, o1, o2)                                                             \
  static void t_##r(void *p) {                                                   \
    register uintptr_t a __asm__(#r) = (uintptr_t)p;                             \
    register uintptr_t s1 __asm__(#o1) = 0x1111;                                 \
    register uintptr_t s2 __asm__(#o2) = 0x2222;                                 \
    uintptr_t a0 = a;                                                            \
    __asm__ volatile("sub $128, %%rsp\n" STUB(r) "\nadd $128, %%rsp"             \
                     : "+r"(a), "+r"(s1), "+r"(s2)                               \
                     :                                                           \
                     : "cc", "memory");                                          \
    CHECK(a == a0 && s1 == 0x1111 && s2 == 0x2222);                              \
  }
T(rax, rcx, rbx)
T(rcx, rax, r12)
T(rdi, rsi, r15)
T(r11, r10, rbx)
T(r15, rax, r14)
#define NREG 5
static void (*const fns[])(void *) = {t_rax, t_rcx, t_rdi, t_r11, t_r15};
#if defined(__APPLE__)
#define TSTUB(r) "call _rt_cardt_" #r
#else
#define TSTUB(r) "call rt_cardt_" #r
#endif
static void tt(void *p) {
  register uintptr_t a __asm__("rdi") = (uintptr_t)p;
  register uintptr_t s1 __asm__("rsi") = 0x1111;
  register uintptr_t s2 __asm__("rbx") = 0x2222;
  uintptr_t a0 = a;
  __asm__ volatile("sub $128, %%rsp\n" TSTUB(rdi) "\nadd $128, %%rsp" : "+r"(a), "+r"(s1), "+r"(s2) : : "cc", "memory");
  CHECK(a == a0 && s1 == 0x1111 && s2 == 0x2222);
}
#endif

void marmot_main(void) {
#if RT_ARCH_ARM64 || RT_ARCH_AMD64
  for (int i = 0; i < NREG; i++) {
    void *p = &word[(i * 8) % 64];
    uint8_t *c = card_of(p);
    *c = 0xff;
    fns[i](p);
    CHECK(*c == 0);  // set: cleared
    fns[i](p);
    CHECK(*c == 0);  // clear: stays clear
  }
  // Addresses past the table's span map onto it (the index is masked).
  uintptr_t far = ((uintptr_t)1 << (RT_CARD_SHIFT + RT_CARD_BITS)) + (uintptr_t)&word[0];
  uint8_t *c = card_of((void *)far);
  *c = 0xff;
  fns[0]((void *)far);
  CHECK(*c == 0);
  // rt_cardt_*: nothing while not marking, the card cleared while marking.
  c = card_of(&word[3]);
  *c = 0xff;
  tt(&word[3]);
  CHECK(*c == 0xff);
  rt_gc_marking = 1;
  tt(&word[3]);
  rt_gc_marking = 0;
  CHECK(*c == 0);
#endif
  out("ok\n");
}

// arch_arm64.S: context switch and setjmp/longjmp for arm64 (darwin tested, linux untested).
// x18 is the platform register on darwin and is never touched.

#if defined(__aarch64__)

#if defined(__APPLE__)
#define SYM(x) _##x
#define FUNC(x) .globl SYM(x) %% .private_extern SYM(x) %% .p2align 2 %% SYM(x):
#define PUBFUNC(x) .globl SYM(x) %% .p2align 2 %% SYM(x):
#else
#define SYM(x) x
#define FUNC(x) .globl x ; .hidden x ; .type x, %function ; .p2align 2 ; x:
#define PUBFUNC(x) .globl x ; .type x, %function ; .p2align 2 ; x:
#endif

.text

// void rt_ctx_switch(void **save, void *to)
FUNC(rt_ctx_switch)
  sub sp, sp, #160
  stp x19, x20, [sp, #0]
  stp x21, x22, [sp, #16]
  stp x23, x24, [sp, #32]
  stp x25, x26, [sp, #48]
  stp x27, x28, [sp, #64]
  stp x29, x30, [sp, #80]
  stp d8, d9, [sp, #96]
  stp d10, d11, [sp, #112]
  stp d12, d13, [sp, #128]
  stp d14, d15, [sp, #144]
  mov x9, sp
  str x9, [x0]
  mov sp, x1
  ldp x19, x20, [sp, #0]
  ldp x21, x22, [sp, #16]
  ldp x23, x24, [sp, #32]
  ldp x25, x26, [sp, #48]
  ldp x27, x28, [sp, #64]
  ldp x29, x30, [sp, #80]
  ldp d8, d9, [sp, #96]
  ldp d10, d11, [sp, #112]
  ldp d12, d13, [sp, #128]
  ldp d14, d15, [sp, #144]
  add sp, sp, #160
  ret

// void *rt_ctx_init(void *top, void (*fn)(void *), void *arg)
// Frame matches rt_ctx_switch: x19 = fn, x20 = arg, x29 = 0, x30 = ctx_start.
FUNC(rt_ctx_init)
  sub x0, x0, #160
  stp x1, x2, [x0, #0]
  stp xzr, xzr, [x0, #16]
  stp xzr, xzr, [x0, #32]
  stp xzr, xzr, [x0, #48]
  stp xzr, xzr, [x0, #64]
  adr x9, Lctx_start
  stp xzr, x9, [x0, #80]
  stp xzr, xzr, [x0, #96]
  stp xzr, xzr, [x0, #112]
  stp xzr, xzr, [x0, #128]
  stp xzr, xzr, [x0, #144]
  ret

Lctx_start:
  mov x0, x20
  mov x29, xzr
  blr x19
  brk #0x1

// void *rt_spill_regs(void *buf): store callee-saved registers, return sp.
FUNC(rt_spill_regs)
  stp x19, x20, [x0, #0]
  stp x21, x22, [x0, #16]
  stp x23, x24, [x0, #32]
  stp x25, x26, [x0, #48]
  stp x27, x28, [x0, #64]
  stp x29, x30, [x0, #80]
  stp d8, d9, [x0, #96]
  stp d10, d11, [x0, #112]
  stp d12, d13, [x0, #128]
  stp d14, d15, [x0, #144]
  mov x0, sp
  ret

// int32_t rt_setjmp(void *buf)
// buf: x19..x28, x29, x30, sp, d8..d15 (21 words of RT_JMPBUF_WORDS).
PUBFUNC(rt_setjmp)
  stp x19, x20, [x0, #0]
  stp x21, x22, [x0, #16]
  stp x23, x24, [x0, #32]
  stp x25, x26, [x0, #48]
  stp x27, x28, [x0, #64]
  stp x29, x30, [x0, #80]
  mov x9, sp
  str x9, [x0, #96]
  stp d8, d9, [x0, #104]
  stp d10, d11, [x0, #120]
  stp d12, d13, [x0, #136]
  stp d14, d15, [x0, #152]
  mov w0, #0
  ret

// void rt_longjmp(void *buf, int32_t val)
PUBFUNC(rt_longjmp)
  ldp x19, x20, [x0, #0]
  ldp x21, x22, [x0, #16]
  ldp x23, x24, [x0, #32]
  ldp x25, x26, [x0, #48]
  ldp x27, x28, [x0, #64]
  ldp x29, x30, [x0, #80]
  ldr x9, [x0, #96]
  mov sp, x9
  ldp d8, d9, [x0, #104]
  ldp d10, d11, [x0, #120]
  ldp d12, d13, [x0, #136]
  ldp d14, d15, [x0, #152]
  cmp w1, #0
  csinc w0, w1, wzr, ne
  ret

#endif

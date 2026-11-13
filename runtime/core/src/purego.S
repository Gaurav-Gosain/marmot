// purego.S: calls to C function pointers and C-callable callbacks for the
// purego package (github.com/ebitengine/purego), whose own assembly marmot
// does not build. marmot's overlay of purego calls these.
//
// rt_purego_syscall(a) calls a->fn with purego's argument block:
//
//   offset   0  fn
//   offset   8  a1 .. a32   integer words (registers first, then the stack)
//   offset 264  f1 .. f8    float registers (the bits of a double)
//   offset 328  r8          arm64: the indirect result register x8
//
// and stores the results back into the block as purego reads them: a1 and
// a2 the two integer result registers, f1.. the float result registers
// (f1, f2 on amd64; f1..f4 on arm64), a3 errno on darwin and 0 elsewhere.
// The call is bracketed like a cgo call (rt_cgo_enter/rt_cgo_exit), so a
// blocking C function does not hold a P and a callback can enter Go.
//
// rt_purego_callback_entry(i) is the C function pointer of callback i. The
// C call reaches a shared routine with the index, which saves the argument
// registers in a block followed directly by the caller's stack arguments
// (floats f0..f7, then the integer registers, then the stack), and calls
// the Go function rt_purego_go_callback (defined by the overlay) with
// purego's callbackArgs {index, args, result[4]} inside the callback
// bracket (rt_cgo_callback_enter/exit). The integer results come back from
// result[0], result[1], the float results from result[2], result[3]
// (amd64) or result[0..3] (arm64), as purego's callbackWrap stores them.

#define PUREGO_MAX_CALLBACKS 2000

#if defined(__APPLE__)
#define SYM(x) _##x
#else
#define SYM(x) x
#endif

#if defined(__x86_64__)

#if defined(__APPLE__)
#define FUNC(x) .globl SYM(x) ; .private_extern SYM(x) ; .p2align 4 ; SYM(x):
#else
#define FUNC(x) .globl x ; .hidden x ; .type x, @function ; .p2align 4 ; x:
#endif

.text

// void rt_purego_syscall(uintptr_t *a)
FUNC(rt_purego_syscall)
  pushq %rbp
  movq %rsp, %rbp
  pushq %rbx
  pushq %r12                  // rsp is 16-byte aligned from here on
  movq %rdi, %rbx             // the block
  call SYM(rt_cgo_enter)
  subq $208, %rsp             // a7..a32 on the stack
  xorl %ecx, %ecx
1:
  movq 56(%rbx,%rcx,8), %rax  // a7 is at offset 56
  movq %rax, (%rsp,%rcx,8)
  incl %ecx
  cmpl $26, %ecx
  jne 1b
  movq 264(%rbx), %xmm0
  movq 272(%rbx), %xmm1
  movq 280(%rbx), %xmm2
  movq 288(%rbx), %xmm3
  movq 296(%rbx), %xmm4
  movq 304(%rbx), %xmm5
  movq 312(%rbx), %xmm6
  movq 320(%rbx), %xmm7
  movq 8(%rbx), %rdi
  movq 16(%rbx), %rsi
  movq 24(%rbx), %rdx
  movq 32(%rbx), %rcx
  movq 40(%rbx), %r8
  movq 48(%rbx), %r9
  movq 0(%rbx), %r10
  movl $8, %eax               // variadic callees: up to 8 vector registers
  call *%r10
  movq %rax, 8(%rbx)
  movq %rdx, 16(%rbx)
  movq %xmm0, 264(%rbx)
  movq %xmm1, 272(%rbx)
#if defined(__APPLE__)
  call ___error
  movl (%rax), %eax
  movq %rax, 24(%rbx)
#else
  movq $0, 24(%rbx)
#endif
  addq $208, %rsp
  call SYM(rt_cgo_exit)
  popq %r12
  popq %rbx
  popq %rbp
  ret

// void *rt_purego_callback_entry(intptr_t i)
FUNC(rt_purego_callback_entry)
  xorl %eax, %eax
  cmpq $PUREGO_MAX_CALLBACKS, %rdi
  jae 1f
  leaq Lpurego_callbacks(%rip), %rax
  shlq $4, %rdi
  addq %rdi, %rax
1:
  ret

// The shared callback routine. On entry: r11 the index, (%rsp) the C
// caller's return address, the caller's stack arguments from 8(%rsp).
// The block is the 14 words below 8(%rsp): it ends where the stack
// arguments begin, over the return address slot, which is restored before
// returning.
.p2align 4
Lpurego_callback:
  movq (%rsp), %r10           // the return address
  leaq -104(%rsp), %rax       // the block (16-byte aligned)
  subq $184, %rsp             // the block, then an 80 byte frame below it
  movq %rbp, 48(%rsp)         // a frame record: saved rbp, return address
  movq %r10, 56(%rsp)
  leaq 48(%rsp), %rbp
  movq %rbx, 64(%rsp)
  movq %xmm0, 0(%rax)
  movq %xmm1, 8(%rax)
  movq %xmm2, 16(%rax)
  movq %xmm3, 24(%rax)
  movq %xmm4, 32(%rax)
  movq %xmm5, 40(%rax)
  movq %xmm6, 48(%rax)
  movq %xmm7, 56(%rax)
  movq %rdi, 64(%rax)
  movq %rsi, 72(%rax)
  movq %rdx, 80(%rax)
  movq %rcx, 88(%rax)
  movq %r8, 96(%rax)
  movq %r9, 104(%rax)         // the return address slot
  movq %r11, 0(%rsp)          // callbackArgs.index
  movq %rax, 8(%rsp)          // callbackArgs.args
  movq %rdi, 16(%rsp)         // result[0]: the struct result pointer, if any
  movq $0, 24(%rsp)
  movq $0, 32(%rsp)
  movq $0, 40(%rsp)
  movq %rsp, %rbx
  call SYM(rt_cgo_callback_enter)
  movl %eax, 72(%rsp)         // the bracket token
  movq %rbx, %rdi
  call SYM(rt_purego_go_callback)
  movl 72(%rsp), %edi
  call SYM(rt_cgo_callback_exit)
  movq 16(%rbx), %rax
  movq 24(%rbx), %rdx
  movq 32(%rbx), %xmm0
  movq 40(%rbx), %xmm1
  movq 56(%rsp), %r10
  movq 48(%rsp), %rbp
  movq 64(%rsp), %rbx
  addq $184, %rsp
  movq %r10, (%rsp)           // the return address slot again
  ret

// The callback entries, 16 bytes each: the index into r11, then the
// shared routine.
.p2align 4
Lpurego_callbacks:
  .set Lpurego_i, 0
  .rept PUREGO_MAX_CALLBACKS
  .p2align 4
  movl $Lpurego_i, %r11d
  jmp Lpurego_callback
  .set Lpurego_i, Lpurego_i+1
  .endr

#elif defined(__aarch64__)

#if defined(__APPLE__)
#define FUNC(x) .globl SYM(x) %% .private_extern SYM(x) %% .p2align 2 %% SYM(x):
#else
#define FUNC(x) .globl x ; .hidden x ; .type x, %function ; .p2align 2 ; x:
#endif

.text

// void rt_purego_syscall(uintptr_t *a)
FUNC(rt_purego_syscall)
  stp x29, x30, [sp, #-32]!
  mov x29, sp
  stp x19, x20, [sp, #16]
  mov x19, x0                 // the block
  bl SYM(rt_cgo_enter)
  sub sp, sp, #192            // a9..a32 on the stack
  add x10, x19, #72           // a9 is at offset 72
  mov x11, sp
  mov x12, #12
1:
  ldp x13, x14, [x10], #16
  stp x13, x14, [x11], #16
  subs x12, x12, #1
  b.ne 1b
  ldp d0, d1, [x19, #264]
  ldp d2, d3, [x19, #280]
  ldp d4, d5, [x19, #296]
  ldp d6, d7, [x19, #312]
  ldp x0, x1, [x19, #8]
  ldp x2, x3, [x19, #24]
  ldp x4, x5, [x19, #40]
  ldp x6, x7, [x19, #56]
  ldr x8, [x19, #328]
  ldr x16, [x19, #0]
  blr x16
  stp x0, x1, [x19, #8]
  stp d0, d1, [x19, #264]
  stp d2, d3, [x19, #280]
#if defined(__APPLE__)
  bl ___error
  ldr w0, [x0]
  str x0, [x19, #24]
#else
  str xzr, [x19, #24]
#endif
  add sp, sp, #192
  bl SYM(rt_cgo_exit)
  ldp x19, x20, [sp, #16]
  ldp x29, x30, [sp], #32
  ret

// void *rt_purego_callback_entry(intptr_t i)
FUNC(rt_purego_callback_entry)
  cmp x0, #PUREGO_MAX_CALLBACKS
  b.hs 1f
  adr x1, Lpurego_callbacks    // the same section, well within 1 MB
  add x0, x1, x0, lsl #3
  ret
1:
  mov x0, #0
  ret

// The shared callback routine. On entry: x9 the index, x30 the C caller's
// return address, the caller's stack arguments from sp. The block is the
// 16 words below sp, so the stack arguments follow it.
.p2align 2
Lpurego_callback:
  sub sp, sp, #208            // an 80 byte frame, then the block
  stp x29, x30, [sp, #48]
  add x29, sp, #48
  stp x19, x20, [sp, #64]
  add x10, sp, #80            // the block
  stp d0, d1, [x10, #0]
  stp d2, d3, [x10, #16]
  stp d4, d5, [x10, #32]
  stp d6, d7, [x10, #48]
  stp x0, x1, [x10, #64]
  stp x2, x3, [x10, #80]
  stp x4, x5, [x10, #96]
  stp x6, x7, [x10, #112]
  stp x9, x10, [sp, #0]       // callbackArgs.index, callbackArgs.args
  stp x8, xzr, [sp, #16]      // result[0]: the indirect result pointer x8
  stp xzr, xzr, [sp, #32]
  mov x19, sp
  bl SYM(rt_cgo_callback_enter)
  mov w20, w0                 // the bracket token
  mov x0, x19
  bl SYM(rt_purego_go_callback)
  mov w0, w20
  bl SYM(rt_cgo_callback_exit)
  ldp x0, x1, [x19, #16]
  ldp d0, d1, [x19, #16]
  ldp d2, d3, [x19, #32]
  ldp x19, x20, [sp, #64]
  ldp x29, x30, [sp, #48]
  add sp, sp, #208
  ret

// The callback entries, 8 bytes each: the index into x9, then the shared
// routine.
.p2align 3
Lpurego_callbacks:
  .set Lpurego_i, 0
  .rept PUREGO_MAX_CALLBACKS
  movz x9, #Lpurego_i
  b Lpurego_callback
  .set Lpurego_i, Lpurego_i+1
  .endr

#endif

#if defined(__linux__)
.section .note.GNU-stack,"",@progbits
#endif

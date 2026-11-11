// arch_amd64.S: context switch and setjmp/longjmp for x86_64.
// UNTESTED: written for linux/amd64 and darwin/amd64, compile-checked only.

#if defined(__x86_64__)

#if defined(__APPLE__)
#define SYM(x) _##x
#define FUNC(x) .globl SYM(x) ; .private_extern SYM(x) ; .p2align 4 ; SYM(x):
#define PUBFUNC(x) .globl SYM(x) ; .p2align 4 ; SYM(x):
#else
#define SYM(x) x
#define FUNC(x) .globl x ; .hidden x ; .type x, @function ; .p2align 4 ; x:
#define PUBFUNC(x) .globl x ; .type x, @function ; .p2align 4 ; x:
#endif

.text

// void rt_ctx_switch(void **save, void *to)   rdi = save, rsi = to
// Frame (low to high): r15 r14 r13 r12 rbx rbp, return address.
FUNC(rt_ctx_switch)
  pushq %rbp
  pushq %rbx
  pushq %r12
  pushq %r13
  pushq %r14
  pushq %r15
  movq %rsp, (%rdi)
  movq %rsi, %rsp
  popq %r15
  popq %r14
  popq %r13
  popq %r12
  popq %rbx
  popq %rbp
  ret

// void *rt_ctx_init(void *top, void (*fn)(void *), void *arg)
// After the switch pops 6 registers and returns into ctx_start, rsp must be
// 16 byte aligned so the call inside ctx_start sees a standard frame.
FUNC(rt_ctx_init)
  movq %rdi, %rax
  andq $-16, %rax
  subq $8, %rax              // return slot at top-8, so rsp is aligned after ret
  leaq Lctx_start(%rip), %rcx
  movq %rcx, (%rax)          // return address
  subq $48, %rax
  movq $0, 0(%rax)           // r15
  movq $0, 8(%rax)           // r14
  movq $0, 16(%rax)          // r13
  movq %rdx, 24(%rax)        // r12 = arg
  movq %rsi, 32(%rax)        // rbx = fn
  movq $0, 40(%rax)          // rbp
  ret

Lctx_start:
  movq %r12, %rdi
  xorl %ebp, %ebp
  call *%rbx
  ud2

// void *rt_spill_regs(void *buf)
FUNC(rt_spill_regs)
  movq %rbx, 0(%rdi)
  movq %rbp, 8(%rdi)
  movq %r12, 16(%rdi)
  movq %r13, 24(%rdi)
  movq %r14, 32(%rdi)
  movq %r15, 40(%rdi)
  leaq 8(%rsp), %rax
  ret

// int32_t rt_setjmp(void *buf): rbx rbp r12 r13 r14 r15 rsp rip
PUBFUNC(rt_setjmp)
  movq %rbx, 0(%rdi)
  movq %rbp, 8(%rdi)
  movq %r12, 16(%rdi)
  movq %r13, 24(%rdi)
  movq %r14, 32(%rdi)
  movq %r15, 40(%rdi)
  leaq 8(%rsp), %rdx
  movq %rdx, 48(%rdi)
  movq (%rsp), %rdx
  movq %rdx, 56(%rdi)
  xorl %eax, %eax
  ret

// void rt_longjmp(void *buf, int32_t val)
PUBFUNC(rt_longjmp)
  movl %esi, %eax
  testl %eax, %eax
  jnz 1f
  movl $1, %eax
1:
  movq 0(%rdi), %rbx
  movq 8(%rdi), %rbp
  movq 16(%rdi), %r12
  movq 24(%rdi), %r13
  movq 32(%rdi), %r14
  movq 40(%rdi), %r15
  movq 48(%rdi), %rsp
  jmpq *56(%rdi)

#endif

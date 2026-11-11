// rt_setjmp/rt_longjmp for the fake runtime: tail calls to the C library, so
// the saved state is that of the caller of rt_setjmp.
#if defined(__APPLE__)
#define NAME(x) _##x
#define LIBC(x) __##x
#else
#define NAME(x) x
#define LIBC(x) _##x
#endif
  .text
  .globl NAME(rt_setjmp)
  .globl NAME(rt_longjmp)
#if defined(__aarch64__)
NAME(rt_setjmp): b LIBC(setjmp)
NAME(rt_longjmp): b LIBC(longjmp)
#else
NAME(rt_setjmp): jmp LIBC(setjmp)
NAME(rt_longjmp): jmp LIBC(longjmp)
#endif

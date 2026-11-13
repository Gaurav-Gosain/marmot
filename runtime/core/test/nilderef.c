// Nil dereference: through the sigpanic hook, and without one.
// expect-exit: 2
// expect-stdout: sigpanic kind 1
// expect-stderr: panic: runtime error: invalid memory address or nil pointer dereference
// wasm: skip (faults are traps, not signals)
#include "rtest.h"

static void *jb[RT_JMPBUF_WORDS];
static void on_fault(uintptr_t addr, int32_t kind) {
  out("sigpanic kind ");
  outu((unsigned long long)kind);
  out("\n");
  rt_longjmp(jb, 1);
}

void marmot_main(void) {
  volatile int *p = (volatile int *)8;
  rt_set_sigpanic(on_fault);
  if (rt_setjmp(jb) == 0) {
    *p = 1;
    CHECK(0);
  }
  rt_set_sigpanic(0);
  *p = 2;
}

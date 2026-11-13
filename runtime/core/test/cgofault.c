// A fault inside a cgo call is fatal even with a sigpanic hook set: a Go
// panic must not unwind through C frames.
// expect-exit: 2
// expect-stderr: signal arrived during cgo execution
#include "rtest.h"

static void on_fault(uintptr_t addr, int32_t kind) {
  out("sigpanic called\n");
  rt_exit(1);
}

void marmot_main(void) {
  volatile int *p = (volatile int *)8;
  rt_set_sigpanic(on_fault);
  rt_cgo_enter_fast();
  *p = 1;
  rt_cgo_exit_fast();
  out("no fault\n");
  rt_exit(1);
}

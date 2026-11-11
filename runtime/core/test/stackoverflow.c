// Unbounded recursion hits the guard page.
// expect-exit: 2
// expect-stderr: fatal error: stack overflow
// env: MARMOT_STACKSIZE=256k
#include "rtest.h"

static int recurse(volatile char *p, int n) {
  volatile char buf[512];
  buf[0] = (char)n;
  return recurse(buf, n + 1) + buf[0] + p[0];
}

static void deep(void *arg) { recurse((volatile char *)"x", 0); }

void marmot_main(void) {
  rt_go(deep, 0);
  rt_sleep(5000000000);
}

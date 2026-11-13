// Cost of the cgo call brackets: rt_cgo_enter/exit (a system call
// transition) and the quick rt_cgo_enter_fast/exit_fast, per call.
#include "rt.h"
#include <stdio.h>

static double now_s(void) { return rt_nanotime() / 1e9; }

void marmot_main(void) {
  const long n = 20000000;
  double t0 = now_s();
  for (long i = 0; i < n; i++) {
    rt_cgo_enter();
    __asm__ volatile("" ::: "memory");
    rt_cgo_exit();
  }
  double t1 = now_s();
  for (long i = 0; i < n; i++) {
    rt_cgo_enter_fast();
    __asm__ volatile("" ::: "memory");
    rt_cgo_exit_fast();
  }
  double t2 = now_s();
  printf("cgo bracketed  %6.2f ns/call\n", (t1 - t0) / n * 1e9);
  printf("cgo fast       %6.2f ns/call\n", (t2 - t1) / n * 1e9);
  fflush(stdout);
  rt_exit(0);
}

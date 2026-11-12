// rt_timer_start's arg is a GC root while the timer is pending: a heap
// object referenced only by the timer survives collections and is intact
// when fn runs (the Go runtime passes fresh timer tokens this way).
// expect-stdout: ok
// env: GOGC=1
#include "rtest.h"
#include <stdatomic.h>

#define N 64
static _Atomic int fired, good;

static void fire(void *arg) {
  long *p = arg;
  long sum = 0;
  for (int i = 0; i < 32; i++) sum += p[i];
  if (sum == 32L * p[32]) atomic_fetch_add(&good, 1);
  atomic_fetch_add(&fired, 1);
}

void marmot_main(void) {
  int64_t now = rt_nanotime();
  for (int k = 0; k < N; k++) {
    long *p = rt_alloc(33 * sizeof(long), RT_ALLOC_CONSERVATIVE, 0, 0);
    for (int i = 0; i < 33; i++) p[i] = k + 7;
    rt_timer_start(now + 20000000 + k * 100000, fire, p);  // only reference
  }
  // Churn the heap so freed tokens would be reused and overwritten.
  while (atomic_load(&fired) < N) {
    for (int i = 0; i < 2000; i++) {
      long *x = rt_alloc(33 * sizeof(long), RT_ALLOC_CONSERVATIVE, 0, 0);
      for (int j = 0; j < 33; j++) x[j] = -1;
    }
    rt_gc_collect();
  }
  CHECK(atomic_load(&good) == N);
  out("ok\n");
}

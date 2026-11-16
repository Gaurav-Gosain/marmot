// Goroutines that exit one after another, some through rt_goexit from deep
// frames, beside goroutines that sleep across the churn. Under JSPI
// (wasip1-wasm-jspi) the engine stacks of exited goroutines are pooled: a new
// goroutine runs on one after a longjmp back to that stack's loop, through
// the frames of the goroutine that exited there.
// expect-stdout: ok
#include "rtest.h"

static rt_mutex mu;
static uint64_t sum, want;
static waitgroup wg;

static void add(uint64_t v) {
  rt_lock(&mu);
  sum += v;
  rt_unlock(&mu);
}

static void quick(void *a) {
  add((uintptr_t)a);
  wg_done(&wg);
}

static volatile int sink;

static __attribute__((noinline)) void deep(int n, uintptr_t a) {
  if (n == 0) {
    add(a);
    wg_done(&wg);
    rt_goexit();
  }
  deep(n - 1, a);
  sink++;  // not a tail call
}

static void deepg(void *a) { deep(40, (uintptr_t)a); }

static void sleeper(void *a) {
  for (int i = 0; i < 5; i++) rt_sleep(200000);
  add((uintptr_t)a);
  wg_done(&wg);
}

void marmot_main(void) {
  uintptr_t v = 1;
  wg_add(&wg, 100);
  for (int i = 0; i < 100; i++, v++) {
    rt_go(sleeper, (void *)v);
    want += v;
  }
  for (int round = 0; round < 3; round++) {
    for (int i = 0; i < 3000; i++, v++) {
      wg_add(&wg, 1);
      rt_go(i % 10 == 0 ? deepg : quick, (void *)v);
      want += v;
      if (i % 50 == 0) rt_gosched();
    }
    rt_gosched();
  }
  wg_wait(&wg);
  CHECK(sum == want);
  out("ok\n");
}

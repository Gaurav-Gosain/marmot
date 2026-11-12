// rt_parallel_for helpers are system goroutines: rt_numgoroutine does not
// count them, during or right after a loop (gc's NumGoroutine sees only
// the program's goroutines). GOMAXPROCS=1 runs the loop inline.
// expect-stdout: ok
// env: GOMAXPROCS=8
#include "rtest.h"
#include <stdatomic.h>

static _Atomic intptr_t seen_max;
static long data[100000];

static void body(void *ctx, int64_t lo, int64_t hi) {
  (void)ctx;
  intptr_t n = rt_numgoroutine();
  intptr_t m = atomic_load(&seen_max);
  while (n > m && !atomic_compare_exchange_weak(&seen_max, &m, n)) {}
  for (int64_t i = lo; i < hi; i++) data[i] = i * 2;
}

void marmot_main(void) {
  CHECK(rt_numgoroutine() == 1);
  for (int r = 0; r < 20; r++) rt_parallel_for(100000, 100, body, 0);
  CHECK(atomic_load(&seen_max) == 1);
  CHECK(rt_numgoroutine() == 1);
  for (int i = 0; i < 100000; i++) CHECK(data[i] == i * 2);
  rt_gomaxprocs(1);
  rt_parallel_for(100000, 100, body, 0);
  CHECK(rt_numgoroutine() == 1);
  out("ok\n");
}

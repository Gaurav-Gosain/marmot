// A goroutine (not only main) can recurse deeply: std's depth-limit tests
// recurse about 10^4-10^5 levels, which gc's growable stacks allow.
// expect-stdout: ok
#include "rtest.h"
#include <stdatomic.h>

static _Atomic int done;
static _Atomic int lockword;
static rt_g *waiter;

static void lockw(void) {
  int z = 0;
  while (!atomic_compare_exchange_weak(&lockword, &z, 1)) z = 0;
}
static void unlock_word(void *p) { atomic_store((_Atomic int *)p, 0); }

__attribute__((noinline)) static long down(long n, volatile char *pad) {
  volatile char buf[200];
  buf[0] = (char)n;
  if (n == 0) return buf[0];
  return down(n - 1, buf) + pad[0];
}

static void worker(void *arg) {
  volatile char x = 1;
  long r = down(100000, &x);  // about 25 MB of frames
  CHECK(r != -1);
  lockw();
  atomic_store(&done, 1);
  rt_g *w = waiter;
  waiter = 0;
  atomic_store(&lockword, 0);
  if (w) rt_ready(w);
}

void marmot_main(void) {
  rt_go(worker, 0);
  lockw();
  if (!atomic_load(&done)) {
    waiter = rt_current();
    rt_park(unlock_word, (void *)&lockword);
  } else {
    atomic_store(&lockword, 0);
  }
  CHECK(atomic_load(&done));
  out("ok\n");
}

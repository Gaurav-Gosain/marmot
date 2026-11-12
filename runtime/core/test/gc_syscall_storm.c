// Collections started by several goroutines at once while others come and
// go from short syscalls (sysmon retakes their Ps). A P must never belong to
// two Ms: an M that returned from a syscall to a P another M had since taken
// into its own syscall used to own it with p->m naming the other M, so stop
// the world signalled the wrong thread, and the M blocked on the GC lock was
// never stopped. A hang shows as a timeout.
// expect-stdout: ok
// env: GOMAXPROCS=2 GOGC=5
// timeout: 120
#include "rtest.h"
#include <stdatomic.h>
#include <stdlib.h>
#include <unistd.h>

#define NG 6

static _Atomic int remaining;
static _Atomic int lockword;
static rt_g *waiter;

static void unlock_word(void *p) { atomic_store((_Atomic int *)p, 0); }
static void lockw(void) {
  int z = 0;
  while (!atomic_compare_exchange_weak(&lockword, &z, 1)) z = 0;
}

static int rounds(void) {
  const char *e = getenv("STORM_ROUNDS");
  return e ? atoi(e) : 3000;
}

static void worker(void *arg) {
  long id = (long)arg;
  void **keep = rt_alloc(64 * sizeof(void *), RT_ALLOC_CONSERVATIVE, 0, 0);
  int n = rounds();
  for (int i = 0; i < n; i++) {
    rt_entersyscall();
    usleep((useconds_t)(15 + (id * 13 + i) % 30));
    rt_exitsyscall();
    for (int k = 0; k < 8; k++) keep[(i * 8 + k) % 64] = rt_alloc(2048, RT_ALLOC_CONSERVATIVE, 0, 0);
  }
  if (atomic_fetch_sub(&remaining, 1) == 1) {
    lockw();
    rt_g *w = waiter;
    waiter = 0;
    atomic_store(&lockword, 0);
    if (w) rt_ready(w);
  }
}

void marmot_main(void) {
  atomic_store(&remaining, NG);
  for (long i = 0; i < NG; i++) rt_go(worker, (void *)i);
  lockw();
  if (atomic_load(&remaining) > 0) {
    waiter = rt_current();
    rt_park(unlock_word, (void *)&lockword);
  } else {
    atomic_store(&lockword, 0);
  }
  out("ok\n");
}

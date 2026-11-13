// One goroutine makes syscalls just longer than sysmon's retake delay
// while main is parked and nothing else runs (GOMAXPROCS=1): rt_exitsyscall
// races with sysmon handing the retaken P off. A lost wakeup hangs it.
// expect-stdout: ok
// env: GOMAXPROCS=1
// timeout: 120
// wasm: skip (needs libc headers)
#include "rtest.h"
#include <stdatomic.h>
#include <stdlib.h>
#include <unistd.h>

static _Atomic int lockword;
static rt_g *waiter;
static int done;
static _Atomic int progress;
static int rt_getenv_n(void) { const char *e = getenv("RETAKE_N"); return e ? atoi(e) : 20000; }

static void unlock_word(void *p) { atomic_store((_Atomic int *)p, 0); }
static void lockw(void) {
  int z = 0;
  while (!atomic_compare_exchange_weak(&lockword, &z, 1)) z = 0;
}

static void worker(void *arg) {
  int n = rt_getenv_n();
  for (int i = 0; i < n; i++) {
    rt_entersyscall();
    usleep((useconds_t)(15 + i % 20));
    rt_exitsyscall();
    atomic_fetch_add(&progress, 1);
  }
  lockw();
  done = 1;
  rt_g *w = waiter;
  waiter = 0;
  atomic_store(&lockword, 0);
  if (w) rt_ready(w);
}

void marmot_main(void) {
  rt_go(worker, 0);
  lockw();
  if (!done) {
    waiter = rt_current();
    rt_park(unlock_word, (void *)&lockword);
  } else {
    atomic_store(&lockword, 0);
  }
  out("ok\n");
}

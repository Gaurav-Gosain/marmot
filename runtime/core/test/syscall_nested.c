// Nested system call brackets (syscall.Syscall calls entersyscall around
// rt_syscall, which brackets itself) while other goroutines collect. Only
// the outermost pair may change the P: an inner enter stored P_SYSCALL over
// the P_GCSTOP that a stop the world had set after the outer one, and the
// stop counted that P twice and woke with another P still running.
// expect-stdout: ok
// env: GOMAXPROCS=4 GOGC=5
// timeout: 120
// wasm: skip (needs libc headers)
#include "rtest.h"
#include <stdatomic.h>
#include <stdlib.h>
#include <unistd.h>

#define NG 8

static _Atomic int remaining;
static _Atomic int lockword;
static rt_g *waiter;

static void unlock_word(void *p) { atomic_store((_Atomic int *)p, 0); }
static void lockw(void) {
  int z = 0;
  while (!atomic_compare_exchange_weak(&lockword, &z, 1)) z = 0;
}

static void worker(void *arg) {
  long id = (long)arg;
  void **keep = rt_alloc(64 * sizeof(void *), RT_ALLOC_CONSERVATIVE, 0, 0);
  for (int i = 0; i < 2000; i++) {
    rt_entersyscall();
    usleep((useconds_t)(5 + (id * 7 + i) % 20));
    rt_entersyscall();
    usleep(5);
    rt_exitsyscall();
    usleep(5);
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

// Lost wakeups around syscalls: many goroutines make syscalls long enough
// for sysmon to retake their P (rt_exitsyscall then finds no P) while
// others park and ready each other. Every goroutine must finish; a lost
// wakeup shows as a hang (timeout).
// expect-stdout: ok
// env: GOMAXPROCS=2
// timeout: 120
// wasm: skip (needs libc headers)
#include "rtest.h"
#include <stdatomic.h>
#include <unistd.h>

#define NG 16
#define ROUNDS 3000

static _Atomic int done;
static rt_g *waiter;
static _Atomic int remaining;
static _Atomic int lockword;

static void unlock_word(void *p) { atomic_store((_Atomic int *)p, 0); }

static void lockw(void) {
  int z = 0;
  while (!atomic_compare_exchange_weak(&lockword, &z, 1)) z = 0;
}

static void worker(void *arg) {
  long id = (long)arg;
  for (int i = 0; i < ROUNDS; i++) {
    rt_entersyscall();
    usleep((useconds_t)(20 + (id * 7 + i) % 40));  // 20-60 us: sysmon retakes after 20 us
    rt_exitsyscall();
    if ((i & 7) == 0) rt_gosched();
  }
  if (atomic_fetch_sub(&remaining, 1) == 1) {
    lockw();
    rt_g *w = waiter;
    waiter = 0;
    atomic_store(&done, 1);
    atomic_store(&lockword, 0);
    if (w) rt_ready(w);
  }
}

// Ping-pong pair: park/ready traffic between Ms without Ps.
static rt_g *pp[2];
static _Atomic int turn;
static _Atomic int stop_pp;
static void pinger(void *arg) {
  long me = (long)arg;
  while (!atomic_load(&stop_pp)) {
    lockw();
    if (atomic_load(&turn) != me) {
      pp[me] = rt_current();
      rt_park(unlock_word, (void *)&lockword);
      continue;
    }
    atomic_store(&turn, 1 - me);
    rt_g *o = pp[1 - me];
    pp[1 - me] = 0;
    atomic_store(&lockword, 0);
    if (o) rt_ready(o);
    rt_entersyscall();
    usleep(25);
    rt_exitsyscall();
  }
  lockw();
  rt_g *o = pp[1 - me];
  pp[1 - me] = 0;
  atomic_store(&lockword, 0);
  if (o) rt_ready(o);
}

void marmot_main(void) {
  atomic_store(&remaining, NG);
  rt_go(pinger, (void *)0);
  rt_go(pinger, (void *)1);
  for (long i = 0; i < NG; i++) rt_go(worker, (void *)i);
  lockw();
  if (!atomic_load(&done)) {
    waiter = rt_current();
    rt_park(unlock_word, (void *)&lockword);
  } else {
    atomic_store(&lockword, 0);
  }
  atomic_store(&stop_pp, 1);
  out("ok\n");
}

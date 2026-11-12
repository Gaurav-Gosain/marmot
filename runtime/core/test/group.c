// Goroutine groups (testing/synctest bubbles): membership is inherited,
// on_idle runs when the last running member parks durably or exits, an
// ordinary park keeps a member counted as running, and rt_ready of a
// durable member counts it again.
// expect-stdout: ok
// env: GOMAXPROCS=4
#include "rtest.h"
#include <stdatomic.h>
#include <unistd.h>

static rt_group *grp;
static _Atomic int idle_calls;
static rt_g *volatile wake_on_idle;  // who on_idle readies
static _Atomic int lockword;

static void lockw(void) {
  int z = 0;
  while (!atomic_compare_exchange_weak(&lockword, &z, 1)) z = 0;
}
static void unlockw(void *p) { atomic_store((_Atomic int *)p, 0); }

static void on_idle(void *arg) {
  (void)arg;
  atomic_fetch_add(&idle_calls, 1);
  rt_g *g = wake_on_idle;
  wake_on_idle = 0;
  if (g) rt_ready(g);
}

static rt_g *volatile main_g;
static rt_g *volatile child_g;
static _Atomic int child_stage;

// Parks durably until on_idle wakes it, then wakes main and exits.
static void child(void *arg) {
  (void)arg;
  lockw();
  child_g = rt_current();
  wake_on_idle = rt_current();
  atomic_store(&child_stage, 1);
  rt_park_durable(unlockw, (void *)&lockword);
  atomic_store(&child_stage, 2);
  lockw();
  rt_g *m = main_g;
  main_g = 0;
  atomic_store(&lockword, 0);
  if (m) rt_ready(m);
}

// Parks with a plain rt_park for a while: keeps the group from going idle.
static rt_g *volatile napper;
static void nap(void *arg) {
  (void)arg;
  lockw();
  napper = rt_current();
  rt_park(unlockw, (void *)&lockword);
}

static void outsider_wake(void *arg) {  // timer callback, outside the group
  (void)arg;
  lockw();
  rt_g *n = napper;
  napper = 0;
  rt_g *m = main_g;
  main_g = 0;
  atomic_store(&lockword, 0);
  if (n) rt_ready(n);
  if (m) rt_ready(m);
}

void marmot_main(void) {
  int64_t run, alive;
  grp = rt_group_new(on_idle, 0);
  rt_group_join(grp);
  rt_group_counts(grp, &run, &alive);
  CHECK(run == 1 && alive == 1);

  rt_go(child, 0);
  while (atomic_load(&child_stage) < 1) rt_gosched();
  // Main parks durably: nobody runs, on_idle wakes the child, which wakes
  // main and exits.
  lockw();
  main_g = rt_current();
  rt_park_durable(unlockw, (void *)&lockword);
  while (atomic_load(&child_stage) < 2) rt_gosched();
  for (int i = 0; i < 1000; i++) {
    rt_group_counts(grp, &run, &alive);
    if (alive == 1) break;
    rt_gosched();
  }
  CHECK(atomic_load(&idle_calls) == 1);
  CHECK(run == 1 && alive == 1);

  // A member in an ordinary park counts as running: no idle call while it
  // naps, even though main parks durably. A timer outside the group wakes
  // both.
  rt_go(nap, 0);
  while (!napper) rt_gosched();
  rt_timer_start(rt_nanotime() + 20000000, outsider_wake, 0);
  lockw();
  main_g = rt_current();
  rt_park_durable(unlockw, (void *)&lockword);
  CHECK(atomic_load(&idle_calls) == 1);
  for (int i = 0; i < 1000; i++) {
    rt_group_counts(grp, &run, &alive);
    if (alive == 1) break;
    rt_gosched();
  }
  CHECK(run == 1 && alive == 1);
  out("ok\n");
}

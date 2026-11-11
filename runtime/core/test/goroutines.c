// Goroutine creation, park/ready, gosched, goexit, numgoroutine, gslot, gparam.
// expect-stdout: done
// env: GOMAXPROCS=1
#include "rtest.h"

static waitgroup wg;
static int counter;
static rt_mutex mu;

static void worker(void *arg) {
  CHECK(*rt_gslot() == 0);
  *rt_gslot() = arg;
  for (int i = 0; i < 10; i++) {
    rt_lock(&mu);
    counter++;
    rt_unlock(&mu);
    rt_gosched();
  }
  CHECK(*rt_gslot() == arg);
  wg_done(&wg);
}

// Ping-pong between two goroutines through park/ready and gparam.
static rt_g *pinger, *ponger;
static rt_mutex pmu;
static int pings;
static void unlock_pmu(void *p) { rt_unlock(&pmu); }

static void pong(void *arg) {
  rt_lock(&pmu);
  ponger = rt_current();
  rt_park(unlock_pmu, 0);
  for (;;) {
    intptr_t v = (intptr_t)*rt_gparam(rt_current());
    if (v < 0) break;
    rt_lock(&pmu);
    *rt_gparam(pinger) = (void *)(v + 1);
    rt_g *p = pinger;
    rt_unlock(&pmu);
    rt_lock(&pmu);
    rt_ready(p);
    rt_park(unlock_pmu, 0);
  }
  wg_done(&wg);
}

static int exited_after;
static void exiter(void *arg) {
  rt_goexit();
  exited_after = 1;
}

void marmot_main(void) {
  CHECK(rt_numgoroutine() == 1);
  CHECK(rt_goid() == 1);
  CHECK(rt_gomaxprocs(0) == 1);
  wg_add(&wg, 100);
  for (intptr_t i = 0; i < 100; i++) rt_go(worker, (void *)(i + 1));
  CHECK(rt_numgoroutine() == 101);
  wg_wait(&wg);
  CHECK(counter == 1000);

  wg_add(&wg, 1);
  rt_go(pong, 0);
  while (!ponger) rt_gosched();
  pinger = rt_current();
  for (intptr_t i = 0; i < 1000; i += 2) {
    rt_lock(&pmu);
    *rt_gparam(ponger) = (void *)i;
    rt_ready(ponger);
    rt_park(unlock_pmu, 0);
    CHECK((intptr_t)*rt_gparam(pinger) == i + 1);
  }
  rt_lock(&pmu);
  *rt_gparam(ponger) = (void *)-1;
  rt_ready(ponger);
  rt_unlock(&pmu);
  wg_wait(&wg);

  rt_go(exiter, 0);
  for (int i = 0; i < 10; i++) rt_gosched();
  CHECK(exited_after == 0);
  // Goroutine structures are reused.
  for (int round = 0; round < 50; round++) {
    wg_add(&wg, 200);
    for (int i = 0; i < 200; i++) rt_go(worker, (void *)1);
    wg_wait(&wg);
  }
  for (int i = 0; i < 1000 && rt_numgoroutine() != 1; i++) rt_gosched();
  CHECK(rt_numgoroutine() == 1);
  out("done\n");
}

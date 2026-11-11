// Micro benchmarks of the runtime core: goroutine switch, spawn, allocation.
// The Go counterparts are in go/micro.go. Run GOMAXPROCS=1 for switch/spawn.
#include "rt.h"
#include <stdio.h>

static double now_s(void) { return rt_nanotime() / 1e9; }

// ---- gosched ping-pong ----
static volatile int stop;
static long yields;
static void yielder(void *arg) {
  while (!stop) {
    yields++;
    rt_gosched();
  }
}

// ---- spawn ----
static rt_mutex wmu;
static long pending;
static rt_g *waiter;
static void wunlock(void *p) { rt_unlock(&wmu); }
static void child(void *arg) {
  rt_lock(&wmu);
  rt_g *g = (--pending == 0) ? waiter : 0;
  if (g) waiter = 0;
  rt_unlock(&wmu);
  if (g) rt_ready(g);
}

// ---- park/ready ping-pong ----
static rt_g *ponger;
static rt_mutex pmu;
static long rounds;
static void punlock(void *p) { rt_unlock(&pmu); }
static rt_g *pinger;
static void pong(void *arg) {
  for (;;) {
    rt_lock(&pmu);
    ponger = rt_current();
    rt_g *p = pinger;
    pinger = 0;
    if (p) rt_ready(p);
    rt_park(punlock, 0);
  }
}

static const uint8_t map2[1] = {0x3};

// rt_handoff ping-pong
static rt_mutex hmu;
static rt_g *hpeer;
static void hunlock(void *p) { rt_unlock(&hmu); }
static void hponger(void *arg) {
  for (;;) {
    rt_lock(&hmu);
    rt_g *p = hpeer;
    hpeer = rt_current();
    if (p)
      rt_handoff(p, hunlock, 0);
    else
      rt_park(hunlock, 0);
  }
}

void marmot_main(void) {
  // gosched between two goroutines
  rt_go(yielder, 0);
  long n = 2000000;
  double t0 = now_s();
  for (long i = 0; i < n; i++) rt_gosched();
  double t1 = now_s();
  stop = 1;
  printf("gosched round trip (2 goroutines): %.1f ns per yield\n", (t1 - t0) * 1e9 / (n + yields)); fflush(stdout);

  // park/ready handoff
  rt_go(pong, 0);
  rt_gosched();
  n = 1000000;
  t0 = now_s();
  for (long i = 0; i < n; i++) {
    rt_lock(&pmu);
    rt_g *p = ponger;
    pinger = rt_current();
    rt_ready(p);
    rt_park(punlock, 0);
  }
  t1 = now_s();
  printf("park/ready handoff: %.1f ns per switch\n", (t1 - t0) * 1e9 / (2.0 * n)); fflush(stdout);

  // handoff
  rt_go(hponger, 0);
  rt_gosched();
  n = 1000000;
  t0 = now_s();
  for (long i = 0; i < n; i++) {
    rt_lock(&hmu);
    rt_g *p = hpeer;
    hpeer = rt_current();
    rt_handoff(p, hunlock, 0);
  }
  t1 = now_s();
  printf("rt_handoff ping-pong: %.1f ns per switch\n", (t1 - t0) * 1e9 / (2.0 * n)); fflush(stdout);

  // spawn and finish
  n = 1000000;
  pending = n;
  t0 = now_s();
  for (long i = 0; i < n; i++) rt_go(child, 0);
  rt_lock(&wmu);
  if (pending > 0) {
    waiter = rt_current();
    rt_park(wunlock, 0);
  } else {
    rt_unlock(&wmu);
  }
  t1 = now_s();
  printf("goroutine spawn+exit: %.1f ns each\n", (t1 - t0) * 1e9 / n); fflush(stdout);

  // allocation throughput (garbage)
  int sizes[] = {16, 64, 256, 1024};
  for (int k = 0; k < 4; k++) {
    n = 20000000 / (k + 1);
    t0 = now_s();
    for (long i = 0; i < n; i++) {
      void **p = rt_alloc((uintptr_t)sizes[k], RT_ALLOC_PRECISE, map2, (uintptr_t)sizes[k]);
      p[0] = p;
    }
    t1 = now_s();
    printf("alloc %4d B (precise, 2 ptr words): %.1f ns each\n", sizes[k], (t1 - t0) * 1e9 / n); fflush(stdout);
  }
  n = 20000000;
  rt_region *rg = rt_region_new();
  t0 = now_s();
  for (long i = 0; i < n; i++) {
    void **p = rt_region_alloc(rg, 16);
    p[0] = p;
    if ((i & 0xffff) == 0xffff) {
      rt_region_free(rg);
      rg = rt_region_new();
    }
  }
  t1 = now_s();
  rt_region_free(rg);
  printf("region alloc 16 B (freed every 64Ki objects): %.1f ns each\n", (t1 - t0) * 1e9 / n); fflush(stdout);
  n = 20000000;
  t0 = now_s();
  for (long i = 0; i < n; i++) ((char *)rt_alloc(32, RT_ALLOC_NOSCAN, 0, 0))[0] = 1;
  t1 = now_s();
  printf("alloc   32 B (noscan): %.1f ns each\n", (t1 - t0) * 1e9 / n); fflush(stdout);
  fflush(stdout);
}

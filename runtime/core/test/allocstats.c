// rt_gc_stats counts allocations exactly (testing.AllocsPerRun): objects
// handed out from per-P caches, objects reused after rt_free, and
// allocations made by other goroutines, with no cache refill in between.
// expect-stdout: ok
// env: GOMAXPROCS=4
#include "rtest.h"
#include <stdatomic.h>

static void *sink[64];
static _Atomic int done;

static void worker(void *arg) {
  (void)arg;
  for (int i = 0; i < 5; i++) sink[i] = rt_alloc(32, RT_ALLOC_NOSCAN, 0, 0);
  atomic_store(&done, 1);
}

static uint64_t mallocs(void) {
  rt_memstats st;
  rt_gc_stats(&st);
  return st.mallocs;
}

void marmot_main(void) {
  sink[0] = rt_alloc(24, RT_ALLOC_CONSERVATIVE, 0, 0);  // warm the cache
  for (int k = 1; k <= 7; k++) {
    uint64_t a = mallocs();
    for (int i = 0; i < k; i++) sink[i] = rt_alloc(24, RT_ALLOC_CONSERVATIVE, 0, 0);
    uint64_t b = mallocs();
    CHECK(b - a == (uint64_t)k);
  }
  // Reuse after rt_free: one free, one more allocation.
  rt_memstats s0, s1;
  void *p = rt_alloc(48, RT_ALLOC_NOSCAN, 0, 0);
  rt_gc_stats(&s0);
  rt_free(p, 48);
  void *q = rt_alloc(48, RT_ALLOC_NOSCAN, 0, 0);
  CHECK(p == q);
  rt_gc_stats(&s1);
  CHECK(s1.mallocs - s0.mallocs == 1 && s1.frees - s0.frees == 1);
  CHECK(s1.total_alloc - s0.total_alloc == 48);
  // Another goroutine's allocations.
  uint64_t a = mallocs();
  rt_go(worker, 0);
  while (!atomic_load(&done)) rt_gosched();
  CHECK(mallocs() - a == 5);
  // Many allocations across cache refills.
  a = mallocs();
  for (int i = 0; i < 100000; i++) sink[i % 64] = rt_alloc(16, RT_ALLOC_NOSCAN, 0, 0);
  CHECK(mallocs() - a == 100000);
  out("ok\n");
}

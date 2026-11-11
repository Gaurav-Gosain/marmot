// Many collections with a small live set over a large heap: the collector
// often finishes marking before helpers join. Regression test for a
// deadlock between late-joining helpers and the collector.
// expect-stdout: ok
// env: GOMAXPROCS=8 MARMOT_GCPROCS=8
// timeout: 120
#include "rtest.h"

static void *keep[64];

void marmot_main(void) {
  for (int round = 0; round < 3000; round++) {
    // Keep the heap above the helper threshold with short-lived big objects.
    void *big = rt_alloc(3 << 20, RT_ALLOC_NOSCAN, 0, 0);
    ((char *)big)[0] = 1;
    keep[round % 64] = rt_alloc(64, RT_ALLOC_CONSERVATIVE, 0, 0);
    rt_gc_collect();
  }
  out("ok\n");
}

// rt_heap_object: 1 for pointers into allocated heap objects (start or
// interior, small or large, fresh from a cached span), 0 for globals,
// stacks, region memory and C memory.
// expect-stdout: ok
#include "rtest.h"
#include <stdlib.h>

static long global_var[4];

void marmot_main(void) {
  long local = 1;
  CHECK(rt_heap_object(global_var) == 0);
  CHECK(rt_heap_object(&local) == 0);
  void *cm = malloc(64);
  CHECK(rt_heap_object(cm) == 0);
  free(cm);
  rt_region *r = rt_region_new();
  CHECK(rt_heap_object(rt_region_alloc(r, 32)) == 0);
  rt_region_free(r);
  for (int i = 0; i < 1000; i++) {
    char *p = rt_alloc(24 + (uintptr_t)i % 200, RT_ALLOC_CONSERVATIVE, 0, 0);
    CHECK(rt_heap_object(p) == 1 && rt_heap_object(p + 7) == 1);
  }
  char *big = rt_alloc(1 << 20, RT_ALLOC_NOSCAN, 0, 0);
  CHECK(rt_heap_object(big) == 1 && rt_heap_object(big + 500000) == 1);
  CHECK(rt_heap_object(0) == 0);
  out("ok\n");
}

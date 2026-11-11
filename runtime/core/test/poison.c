// MARMOT_POISON=1: freed memory holds the pattern, is not reused, and
// collections run cleanly when nothing touches it.
// expect-stdout: ok
// env: MARMOT_POISON=1
#include "rtest.h"

void marmot_main(void) {
  unsigned char *a = rt_alloc(48, RT_ALLOC_NOSCAN, 0, 0);
  memset(a, 1, 48);
  rt_free(a, 48);
  for (int i = 0; i < 48; i++) CHECK(a[i] == 0xFA);
  void *b = rt_alloc(48, RT_ALLOC_NOSCAN, 0, 0);
  CHECK(b != a);  // quarantined, not reused
  for (int r = 0; r < 3; r++) {
    for (int i = 0; i < 20000; i++) {
      void *x = rt_alloc(48, RT_ALLOC_CONSERVATIVE, 0, 0);
      if (i % 3 == 0) rt_free(x, 48);
    }
    rt_gc_collect();  // pattern checks pass
  }
  for (int i = 0; i < 48; i++) CHECK(a[i] == 0xFA);
  void *big = rt_alloc(1 << 20, RT_ALLOC_NOSCAN, 0, 0);
  rt_free(big, 1 << 20);
  rt_gc_collect();
  void *big2 = rt_alloc(1 << 20, RT_ALLOC_NOSCAN, 0, 0);
  CHECK(big2 != big);
  rt_region *rg = rt_region_new();
  for (int i = 0; i < 5000; i++) rt_region_alloc(rg, 64);
  rt_region_free(rg);
  rt_gc_collect();
  out("ok\n");
}

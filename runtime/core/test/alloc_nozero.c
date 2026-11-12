// rt_alloc_nozero: NOSCAN memory that is not zeroed; usable at every size,
// counted like rt_alloc, kept alive by references across collections.
// expect-stdout: ok
// env: GOGC=10
#include "rtest.h"

static unsigned char *keep[256];

void marmot_main(void) {
  rt_memstats s0, s1;
  rt_gc_stats(&s0);
  for (int i = 0; i < 256; i++) {
    uintptr_t n = (uintptr_t)1 << (i % 18);  // 1 B to 128 KiB
    keep[i] = rt_alloc_nozero(n);
    memset(keep[i], i, n);
  }
  rt_gc_stats(&s1);
  CHECK(s1.mallocs - s0.mallocs == 256);
  for (int r = 0; r < 50000; r++) rt_alloc_nozero(100 + (uintptr_t)r % 900);  // garbage
  rt_gc_collect();
  for (int i = 0; i < 256; i++) {
    uintptr_t n = (uintptr_t)1 << (i % 18);
    for (uintptr_t k = 0; k < n; k += 61) CHECK(keep[i][k] == (unsigned char)i);
  }
  // Zeroed allocations after nozero ones still come back zero.
  for (int r = 0; r < 1000; r++) {
    unsigned char *z = rt_alloc(512, RT_ALLOC_NOSCAN, 0, 0);
    for (int k = 0; k < 512; k++) CHECK(z[k] == 0);
    memset(rt_alloc_nozero(512), 0xAB, 512);
  }
  out("ok\n");
}

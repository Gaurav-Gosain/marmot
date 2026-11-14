// Free OS pages inside small spans that still hold live objects are
// released; objects allocated there later read as zero, and the live ones
// are intact.
// expect-stdout: ok
// env: GOMAXPROCS=1
#include "rtest.h"

extern uintptr_t gc_scavenge_partial_test(void);

#define N 4000
#define SZ 3000  // class 3072: 24 KiB spans, whole OS pages inside on 4 and 16 KiB pages
static uintptr_t *keep[N / 8];

void marmot_main(void) {
  // Fill spans, keep one object in eight: spans stay in use with free pages.
  for (int i = 0; i < N; i++) {
    uintptr_t *p = rt_alloc(SZ, RT_ALLOC_CONSERVATIVE, 0, 0);
    for (int k = 0; k < SZ / 8; k++) p[k] = 0x5a5a5a5a5a5a5a5aull ^ (uintptr_t)k;
    if (i % 8 == 0) {
      p[0] = (uintptr_t)i;
      keep[i / 8] = p;
    }
  }
  rt_gc_collect();
  uintptr_t got = gc_scavenge_partial_test();
  CHECK(got > 0);
  for (int i = 0; i < N / 8; i++) {
    CHECK(keep[i][0] == (uintptr_t)(i * 8));
    for (int k = 1; k < SZ / 8; k++) CHECK(keep[i][k] == (0x5a5a5a5a5a5a5a5aull ^ (uintptr_t)k));
  }
  // New objects in the released pages are cleared.
  for (int i = 0; i < N; i++) {
    uintptr_t *p = rt_alloc(SZ, RT_ALLOC_CONSERVATIVE, 0, 0);
    for (int k = 0; k < SZ / 8; k++) CHECK(p[k] == 0);
  }
  out("ok\n");
}

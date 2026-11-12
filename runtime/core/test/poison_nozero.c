// MARMOT_POISON=1: rt_alloc_nozero memory holds 0xFB, at small and large
// sizes, also where earlier zeroed or freed objects lived.
// expect-stdout: ok
// env: MARMOT_POISON=1
#include "rtest.h"

void marmot_main(void) {
  for (int r = 0; r < 200; r++) {
    uintptr_t n = (uintptr_t)1 << (r % 18);
    unsigned char *z = rt_alloc(n, RT_ALLOC_NOSCAN, 0, 0);
    rt_free(z, n);
    unsigned char *p = rt_alloc_nozero(n);
    for (uintptr_t k = 0; k < n; k++) CHECK(p[k] == 0xFB);
  }
  out("ok\n");
}

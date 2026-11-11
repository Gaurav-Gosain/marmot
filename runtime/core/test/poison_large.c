// MARMOT_POISON=1: an access to a large object after rt_free faults and is
// named as a use after free.
// expect-exit: 2
// expect-stderr: MARMOT_POISON: use after free: access to
// env: MARMOT_POISON=1
#include "rtest.h"

static char *volatile keep;

void marmot_main(void) {
  char *p = rt_alloc(256 << 10, RT_ALLOC_NOSCAN, 0, 0);
  keep = p;
  rt_free(p, 256 << 10);
  out(keep[100000] ? "x\n" : "y\n");
}

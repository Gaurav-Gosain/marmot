// MARMOT_POISON=1: an access to region memory after rt_region_free faults
// and is named.
// expect-exit: 2
// expect-stderr: in a freed region chunk
// env: MARMOT_POISON=1
#include "rtest.h"

static char *volatile keep;

void marmot_main(void) {
  rt_region *r = rt_region_new();
  char *p = 0;
  for (int i = 0; i < 1000; i++) p = rt_region_alloc(r, 48);  // past the first page
  keep = p;
  rt_region_free(r);
  out(keep[0] ? "x\n" : "y\n");
}

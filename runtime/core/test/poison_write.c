// MARMOT_POISON=1: a write to an object after rt_free is reported at the
// next collection.
// expect-exit: 2
// expect-stderr: MARMOT_POISON: write after free
// env: MARMOT_POISON=1
#include "rtest.h"

static long *volatile keep;

void marmot_main(void) {
  long *p = rt_alloc(64, RT_ALLOC_NOSCAN, 0, 0);
  keep = p;
  rt_free(p, 64);
  keep[3] = 42;  // use after free
  rt_gc_collect();
  out("not detected\n");
}

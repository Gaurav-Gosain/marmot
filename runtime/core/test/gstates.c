// rt_gstates counts live user goroutines by state.
// expect-stdout: running 1 waiting 2
#include "rtest.h"
#include <stdio.h>

static void blocker(void *arg) { rt_park(0, 0); }

void marmot_main(void) {
  rt_go(blocker, 0);
  rt_go(blocker, 0);
  for (int i = 0; i < 100; i++) rt_gosched();
  int64_t st[3];
  rt_gstates(st);
  printf("running %lld waiting %lld\n", (long long)st[0], (long long)st[2]);
  fflush(stdout);
  rt_exit(0);
}

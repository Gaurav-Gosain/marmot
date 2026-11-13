// rt_gstates counts live user goroutines by state.
// expect-stdout: running 1 waiting 2
// wasm: skip (needs libc headers)
#include "rtest.h"
#include <stdio.h>

static void blocker(void *arg) { rt_park(0, 0); }

void marmot_main(void) {
  rt_go(blocker, 0);
  rt_go(blocker, 0);
  // With several Ps the blockers park on other threads: wait for them.
  int64_t st[3];
  for (int i = 0; i < 100000; i++) {
    rt_gosched();
    rt_gstates(st);
    if (st[2] == 2) break;
  }
  printf("running %lld waiting %lld\n", (long long)st[0], (long long)st[2]);
  fflush(stdout);
  rt_exit(0);
}

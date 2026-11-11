// Deadlock detection with several threads.
// expect-exit: 2
// expect-stderr: fatal error: all goroutines are asleep - deadlock!
// env: GOMAXPROCS=4
#include "rtest.h"

static void blocker(void *arg) {
  rt_sleep(1000000);
  rt_park(0, 0);
}

void marmot_main(void) {
  for (int i = 0; i < 20; i++) rt_go(blocker, 0);
  rt_park(0, 0);
}

// All goroutines blocked: fatal deadlock error, exit 2.
// expect-exit: 2
// expect-stderr: fatal error: all goroutines are asleep - deadlock!
#include "rtest.h"

static void blocker(void *arg) { rt_park(0, 0); }

void marmot_main(void) {
  rt_go(blocker, 0);
  rt_go(blocker, 0);
  rt_park(0, 0);
}

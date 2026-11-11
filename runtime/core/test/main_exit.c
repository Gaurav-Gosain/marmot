// Returning from main exits 0 even with goroutines running or blocked.
// expect-stdout: bye
#include "rtest.h"

static void spin(void *arg) { for (;;) rt_gosched(); }
static void blocked(void *arg) { rt_park(0, 0); }

void marmot_main(void) {
  rt_go(spin, 0);
  rt_go(blocked, 0);
  rt_gosched();
  out("bye\n");
}

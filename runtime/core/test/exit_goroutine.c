// rt_exit from a goroutine other than main, while main sleeps: the process
// exits with its status (under JSPI from an engine stack that is neither
// _start's nor main's).
// expect-exit: 5
#include "rtest.h"

static void worker(void *arg) { rt_exit(5); }

void marmot_main(void) {
  rt_go(worker, 0);
  for (;;) rt_sleep(1000000);
}

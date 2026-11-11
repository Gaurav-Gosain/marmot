// Main calls rt_goexit: others keep running; then the no-goroutines error.
// expect-exit: 2
// expect-stdout: worker ran
// expect-stderr: fatal error: no goroutines (main called runtime.Goexit) - deadlock!
#include "rtest.h"

static void worker(void *arg) {
  rt_sleep(1000000);
  out("worker ran\n");
}

void marmot_main(void) {
  rt_go(worker, 0);
  rt_goexit();
}

// MARMOT_DEADLOCK_DEBUG=2: with no scheduling progress for
// MARMOT_DEADLOCK_SECS the watchdog prints every M, P and G and exits 2.
// Here main sits in one long syscall, so nothing is scheduled.
// expect-exit: 2
// expect-stderr: no scheduling progress for 1 s
// env: MARMOT_DEADLOCK_DEBUG=2 MARMOT_DEADLOCK_SECS=1
// timeout: 30
#include "rtest.h"
#include <unistd.h>

void marmot_main(void) {
  rt_entersyscall();
  sleep(20);
  rt_exitsyscall();
  out("no dump\n");
}

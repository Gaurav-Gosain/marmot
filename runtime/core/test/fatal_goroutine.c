// rt_fatal from a goroutine other than main, while main sleeps: the message
// and status 2 (under JSPI from an engine stack that is not main's).
// expect-exit: 2
// expect-stderr: fatal error: boom
#include "rtest.h"

static void worker(void *arg) { rt_fatal("boom", 4); }

void marmot_main(void) {
  rt_go(worker, 0);
  for (;;) rt_sleep(1000000);
}

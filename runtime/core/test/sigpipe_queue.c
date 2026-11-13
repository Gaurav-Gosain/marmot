// rt_sigpipe with SIGPIPE enabled for os/signal queues it instead of dying;
// with SIGPIPE ignored it returns.
// expect-stdout: ok
// wasm: skip (needs libc headers)
#include "rtest.h"
#include <signal.h>

void marmot_main(void) {
  CHECK(rt_signal_enable(SIGPIPE) == 0);
  rt_sigpipe();
  CHECK(rt_signal_recv() == SIGPIPE);
  CHECK(rt_signal_ignore(SIGPIPE) == 0);
  rt_sigpipe();
  out("ok\n");
}

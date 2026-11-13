// A program that asked for SIGTERM (signal.Notify) but never receives it,
// as when its scheduler is stuck, still ends on SIGTERM: the runtime gives
// the default action after 3 s. The shell reports death by SIGTERM as 143.
// expect-exit: 143
// expect-stderr: not received by the program for 3 s
// timeout: 30
// wasm: skip (needs libc headers)
#include "rtest.h"
#include <signal.h>
#include <unistd.h>

void marmot_main(void) {
  CHECK(rt_signal_enable(SIGTERM) == 0);
  kill(getpid(), SIGTERM);
  // Nobody calls rt_signal_recv; keep the scheduler busy in a syscall.
  for (int i = 0; i < 200; i++) {
    rt_entersyscall();
    usleep(50000);
    rt_exitsyscall();
  }
  out("not killed\n");
}

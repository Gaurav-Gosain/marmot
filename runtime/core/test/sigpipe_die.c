// rt_sigpipe with SIGPIPE not taken by the Go side: the process dies from
// SIGPIPE (the shell reports 141), like gc after EPIPE on fd 1 or 2.
// expect-exit: 141
#include "rtest.h"

void marmot_main(void) {
  rt_sigpipe();
  out("survived\n");
}

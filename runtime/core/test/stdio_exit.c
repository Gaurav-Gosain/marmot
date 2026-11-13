// rt_exit flushes C stdio on darwin (as gc's exit through libc does): a
// buffered printf without fflush still reaches a piped stdout.
// expect-stdout: buffered
#include "rtest.h"
#include <stdio.h>

void marmot_main(void) {
#ifdef __APPLE__
  printf("buffered\n");
#else
  out("buffered\n");
#endif
  rt_exit(0);
}

// rt_fatal prints the message and exits with status 2.
// expect-exit: 2
// expect-stderr: fatal error: boom
#include "rt.h"

void marmot_main(void) { rt_fatal("boom", 4); }

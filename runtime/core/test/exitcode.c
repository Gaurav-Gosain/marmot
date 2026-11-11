// rt_exit sets the process exit status.
// expect-exit: 3
#include "rt.h"

void marmot_main(void) { rt_exit(3); }

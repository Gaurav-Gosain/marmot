// With MARMOT_OUTPUT=json a fatal error is one JSON object, exit 2.
// expect-exit: 2
// env: MARMOT_OUTPUT=json
// expect-stderr: {"schema":1,"kind":"fatal","code":"M0021","message":"bad \"q\"\u000a","goroutines":{"running":1,"runnable":0,"waiting":0},"exitCode":2}
#include "rt.h"

void marmot_main(void) { rt_fatal("bad \"q\"\n", 8); }

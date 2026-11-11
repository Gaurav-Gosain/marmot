// rt_x86_level: 0 off x86, 1 to 4 on x86-64, stable across calls.
// expect-stdout: ok
#include "rtest.h"

void marmot_main(void) {
  int32_t l = rt_x86_level();
#if defined(__x86_64__)
  CHECK(l >= 1 && l <= 4);
#else
  CHECK(l == 0);
#endif
  CHECK(rt_x86_level() == l);
  out("ok\n");
}

// rt_typelinks finds the type link section (here made with a section
// attribute) with all its entries.
// expect-stdout: ok
#include "rtest.h"

#if defined(__APPLE__)
#define TL_SECTION "__DATA,__marmot_tlinks"
#else
#define TL_SECTION "marmot_tlinks"
#endif

static int t1, t2, t3;
__attribute__((used, retain, section(TL_SECTION))) static void *links[3] = {&t1, &t2, &t3};

void marmot_main(void) {
  intptr_t n = -1;
  void **p = rt_typelinks(&n);
  CHECK(p != 0 && n == 3);
  CHECK(p[0] == &t1 && p[1] == &t2 && p[2] == &t3);
  (void)links;
  out("ok\n");
}

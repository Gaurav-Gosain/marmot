// rt_pcln finds a pc table section in the binary (here one made with a
// section attribute) and the image base.
// expect-stdout: ok
#include "rtest.h"
#include <string.h>

#if defined(__APPLE__)
#define PCLN_SECTION "__TEXT,__marmot_pcln"
#else
#define PCLN_SECTION ".marmot_pcln"
#endif

__attribute__((used, section(PCLN_SECTION))) static const uint8_t table[64] = "MRMTPCLN-test-table";

void marmot_main(void) {
  const uint8_t *t = 0;
  intptr_t n = 0;
  uintptr_t base = 0;
  CHECK(rt_pcln(&t, &n, &base) == 1);
  CHECK(n >= 64 && memcmp(t, "MRMTPCLN-test-table", 19) == 0);
  // The base is at or below the code, within the image.
  uintptr_t pc = (uintptr_t)&marmot_main;
  CHECK(base != 0 && base <= pc && pc - base < (64u << 20));
  // Cached: the same answer again.
  const uint8_t *t2;
  intptr_t n2;
  uintptr_t b2;
  CHECK(rt_pcln(&t2, &n2, &b2) == 1 && t2 == t && n2 == n && b2 == base);
  (void)table;
  out("ok\n");
}

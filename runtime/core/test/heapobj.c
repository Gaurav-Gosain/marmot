// rt_heap_object_span and rt_heap_ptr_word: object start and size from an
// interior pointer, and the pointer words of precise objects (small: heap
// bits, large: the element map repeated).
// expect-stdout: ok
#include "rtest.h"

static const uint8_t map101[1] = {0x5};  // words 0 and 2 of 3

static void check_precise(uintptr_t size) {
  char *p = rt_alloc(size, RT_ALLOC_PRECISE, map101, 24);
  void *base = 0;
  uintptr_t sz = 0;
  CHECK(rt_heap_object_span(p + size / 2, &base, &sz) == RT_HEAPOBJ_PRECISE);
  CHECK(base == p && sz >= size);
  for (uintptr_t off = 0; off < size; off += 8) {
    int want = (off % 24) != 8;
    CHECK(rt_heap_ptr_word(base, off) == want);
  }
}

void marmot_main(void) {
  void *base = 0;
  uintptr_t sz = 0;
  static int global;
  int local;
  CHECK(rt_heap_object_span(&global, &base, &sz) == RT_HEAPOBJ_NONE);
  CHECK(rt_heap_object_span(&local, &base, &sz) == RT_HEAPOBJ_NONE);
  char *n = rt_alloc(100, RT_ALLOC_NOSCAN, 0, 0);
  CHECK(rt_heap_object_span(n + 99, &base, &sz) == RT_HEAPOBJ_NOSCAN);
  CHECK(base == n && sz >= 100);
  char *c = rt_alloc(64, RT_ALLOC_CONSERVATIVE, 0, 0);
  CHECK(rt_heap_object_span(c + 8, &base, &sz) == RT_HEAPOBJ_CONSERVATIVE);
  CHECK(base == c);
  check_precise(24);
  check_precise(24 * 40);
  check_precise(24 * 10000);  // large
  out("ok\n");
}

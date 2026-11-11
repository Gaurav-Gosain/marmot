// The heap stays bounded under garbage, live data survives intact.
// expect-stdout: ok
// env: GOMAXPROCS=1
#include "rtest.h"

typedef struct cell {
  struct cell *next;
  uintptr_t val;
} cell;
static const uint8_t cell_map[1] = {1};

static cell *keep;  // global root (data segment)

void marmot_main(void) {
  // A live list in a global.
  for (uintptr_t i = 0; i < 10000; i++) {
    cell *c = rt_alloc(sizeof(cell), RT_ALLOC_PRECISE, cell_map, sizeof(cell));
    c->val = i;
    c->next = keep;
    keep = c;
  }
  // Lots of garbage, small and large, of all kinds.
  for (int i = 0; i < 2000000; i++) {
    uintptr_t sz = (uintptr_t)(i % 97) * 8 + 8;
    void *p = rt_alloc(sz, (uint32_t)(i % 3), i % 3 == 2 ? cell_map : 0, i % 3 == 2 ? 8 : 0);
    ((uintptr_t *)p)[0] = (uintptr_t)p;
    if (i % 5000 == 0) {
      char *big = rt_alloc(200000 + (uintptr_t)i % 7 * 8192, RT_ALLOC_NOSCAN, 0, 0);
      for (int k = 0; k < 200000; k += 4096) CHECK(big[k] == 0);
      big[100] = 1;
    }
  }
  rt_memstats st;
  rt_gc_stats(&st);
  CHECK(st.num_gc > 5);
  CHECK(st.heap_sys < (64u << 20));
  CHECK(st.mallocs > 2000000 && st.frees > 1500000);
  uintptr_t n = 0, expect = 9999;
  for (cell *c = keep; c; c = c->next, n++, expect--) CHECK(c->val == expect);
  CHECK(n == 10000);
  rt_gc_collect();
  rt_memstats st2;
  rt_gc_stats(&st2);
  CHECK(st2.num_gc == st.num_gc + 1);
  CHECK(st2.heap_alloc < (8u << 20));
  // GOGC off: no collections.
  CHECK(rt_gc_set_percent(-1) == 100);
  for (int i = 0; i < 100000; i++) rt_alloc(64, RT_ALLOC_NOSCAN, 0, 0);
  rt_memstats st3;
  rt_gc_stats(&st3);
  CHECK(st3.num_gc == st2.num_gc);
  CHECK(rt_gc_set_percent(100) == -1);
  out("ok\n");
}

// MARMOT_GC_CONC=1 on a program without a write barrier (rt_wb_kind stays
// the runtime's weak 0): the runtime warns and collects with the world
// stopped, so the live list survives the garbage.
// expect-stdout: ok
// expect-stderr: MARMOT_GC_CONC=1 is ignored: the program has no write barrier
// env: MARMOT_GC_CONC=1 MARMOT_GC_VERIFY=1 GOMAXPROCS=4 GOGC=25
// wasm: skip (no concurrent marking on wasm)
#include "rtest.h"

typedef struct cell {
  struct cell *next;
  uintptr_t val;
} cell;
static const uint8_t cell_map[1] = {1};

static cell *keep;

void marmot_main(void) {
  for (uintptr_t i = 0; i < 10000; i++) {
    cell *c = rt_alloc(sizeof(cell), RT_ALLOC_PRECISE, cell_map, sizeof(cell));
    c->val = i;
    c->next = keep;  // no barrier: concurrent marking would miss these
    keep = c;
  }
  for (int i = 0; i < 500000; i++) {
    void *p = rt_alloc(64, RT_ALLOC_NOSCAN, 0, 0);
    ((uintptr_t *)p)[0] = (uintptr_t)i;
  }
  rt_memstats st;
  rt_gc_stats(&st);
  CHECK(st.num_gc > 2);
  uintptr_t n = 0, expect = 9999;
  for (cell *c = keep; c; c = c->next, n++, expect--) CHECK(c->val == expect);
  CHECK(n == 10000);
  out("ok\n");
}

// MARMOT_GC_VERIFY=1 marks the heap a second time and finds no difference
// while marking stops the world; an object unmarked by hand is reported
// with the object that points to it, and kept.
// expect-stdout: ok
// expect-stderr: gc verify: missed object
// env: MARMOT_GC_VERIFY=1 GOMAXPROCS=2
#include "rtest.h"
#include <stdbool.h>

extern void gc_verify_drop(void *p, bool expect_miss);
extern uint64_t gc_verify_missed;

typedef struct cell {
  struct cell *next;
  uintptr_t val;
} cell;
static const uint8_t cell_map[1] = {1};

static cell *keep;

void marmot_main(void) {
  for (uintptr_t i = 0; i < 1000; i++) {
    cell *c = rt_alloc(sizeof(cell), RT_ALLOC_PRECISE, cell_map, sizeof(cell));
    c->val = i;
    c->next = keep;
    keep = c;
  }
  for (int i = 0; i < 300000; i++) {
    void *p = rt_alloc((uintptr_t)(i % 61) * 8 + 8, (uint32_t)(i % 3), i % 3 == 2 ? cell_map : 0, i % 3 == 2 ? 8 : 0);
    ((uintptr_t *)p)[0] = (uintptr_t)p;
  }
  rt_gc_collect();
  rt_memstats st;
  rt_gc_stats(&st);
  CHECK(st.num_gc > 2);
  CHECK(gc_verify_missed == 0);
  // Drop the mark of the 500th cell: the second mark finds it from the 499th.
  cell *c = keep;
  for (int i = 0; i < 500; i++) c = c->next;
  gc_verify_drop(c, true);
  rt_gc_collect();
  CHECK(gc_verify_missed == 1);
  uintptr_t n = 0, expect = 999;
  for (cell *d = keep; d; d = d->next, n++, expect--) CHECK(d->val == expect);
  CHECK(n == 1000);
  for (int i = 0; i < 100000; i++) rt_alloc(64, RT_ALLOC_NOSCAN, 0, 0);
  rt_gc_collect();
  for (cell *d = keep; d; d = d->next) CHECK(d->val < 1000);
  CHECK(gc_verify_missed == 1);
  out("ok\n");
}

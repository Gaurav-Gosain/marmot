// rt_gslot values survive goroutine structure reuse and stay GC roots.
// expect-stdout: ok
// env: GOMAXPROCS=1
#include "rtest.h"

static int finalized;
static void fin(void *p) { finalized++; }
static void *seen[4];
static int nseen;

static void setter(void *arg) {
  void *obj = rt_alloc(64, RT_ALLOC_NOSCAN, 0, 0);
  rt_gc_set_finalizer(obj, fin);
  *rt_gslot() = obj;
}

static void reader(void *arg) { seen[nseen++] = *rt_gslot(); }

void marmot_main(void) {
  rt_go(setter, 0);
  rt_gosched();
  for (int i = 0; i < 5; i++) {
    rt_gc_collect();
    for (int k = 0; k < 10; k++) rt_gosched();
  }
  CHECK(finalized == 0);  // still referenced from the dead goroutine's slot
  rt_go(reader, 0);       // reuses the structure
  rt_gosched();
  CHECK(nseen == 1 && seen[0] != 0);
  out("ok\n");
}

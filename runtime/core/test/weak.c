// Weak pointers: a registered slot is zeroed by the collection that finds
// its object unreachable (also when the object has a finalizer, which still
// runs), left alone while the object is reachable, and untouched after
// unregistering.
// expect-stdout: ok
// env: GOMAXPROCS=1
#include "rtest.h"

static uintptr_t *slots;  // NOSCAN memory: does not keep the objects alive
static void *strong;
static void *hold[4];  // strong references while setting up (callers register while holding one)
static int finalized;
static void fin(void *o) { (void)o; finalized++; }

__attribute__((noinline)) static void setup(void) {
  slots = rt_alloc(4 * sizeof(uintptr_t), RT_ALLOC_NOSCAN, 0, 0);
  for (int i = 0; i < 4; i++) hold[i] = rt_alloc(64, RT_ALLOC_CONSERVATIVE, 0, 0);
  rt_gc_set_finalizer(hold[2], fin);
  strong = hold[1];
  slots[0] = (uintptr_t)hold[0];      // weak only
  slots[1] = (uintptr_t)hold[1] + 8;  // interior, kept by strong
  slots[2] = (uintptr_t)hold[2];      // weak + finalizer
  slots[3] = (uintptr_t)hold[3];      // unregistered below
  for (int i = 0; i < 4; i++) rt_gc_weak_register(&slots[i]);
  rt_gc_weak_register(&slots[0]);  // twice: no-op
  rt_gc_weak_unregister(&slots[3]);
  for (int i = 0; i < 4; i++) hold[i] = 0;
}

__attribute__((noinline)) static void scrub(void) {
  volatile char pad[4096];
  for (int i = 0; i < 4096; i++) pad[i] = 0;
}

void marmot_main(void) {
  setup();
  scrub();
  uintptr_t s3 = slots[3];
  rt_gc_collect();
  CHECK(rt_gc_weak_load(&slots[0]) == 0);
  CHECK(slots[1] == (uintptr_t)strong + 8);
  CHECK(slots[2] == 0);
  CHECK(slots[3] == s3);
  for (int i = 0; i < 100 && !finalized; i++) {
    rt_gc_collect();
    rt_gosched();
  }
  CHECK(finalized == 1);
  strong = 0;
  scrub();
  rt_gc_collect();
  CHECK(slots[1] == 0);
  out("ok\n");
}

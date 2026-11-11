// Finalizers, interior pointers, registered roots, gslot roots, precise
// non-pointer words, timer arguments.
// expect-stdout: ok
// env: GOMAXPROCS=1
#include "rtest.h"

static int finalized[16];
static void fin(void *obj) { finalized[((uintptr_t *)obj)[0]]++; }

typedef struct { uintptr_t tag; void *ptr; uintptr_t nonptr; } obj;
static const uint8_t obj_map[1] = {0x2};  // only word 1 is a pointer

static obj *mk(uintptr_t tag) {
  obj *o = rt_alloc(sizeof(obj), RT_ALLOC_PRECISE, obj_map, sizeof(obj));
  o->tag = tag;
  rt_gc_set_finalizer(o, fin);
  return o;
}

static uintptr_t hidden_roots[4];
static void *interior;

// Keep values off this frame: do the setup in noinline helpers.
__attribute__((noinline)) static void setup(void) {
  mk(1);                                  // garbage: finalized
  obj *a = mk(2);                         // reachable via registered root
  hidden_roots[0] = (uintptr_t)a;
  obj *b = mk(3);
  interior = (char *)b + 16;              // interior pointer keeps it
  obj *c = mk(4);                         // referenced only from precise non-pointer word
  obj *d = mk(5);
  d->nonptr = (uintptr_t)c;
  hidden_roots[1] = (uintptr_t)d;
  obj *e = mk(6);                         // in gslot
  *rt_gslot() = e;
  obj *f = mk(7);                         // finalizer removed: never runs
  rt_gc_set_finalizer(f, 0);
  obj *g = mk(8);                         // chain: g -> h, both unreachable
  obj *h = mk(9);
  g->ptr = h;
}

__attribute__((noinline)) static void clobber_stack(void) {
  volatile uintptr_t junk[512];
  for (int i = 0; i < 512; i++) junk[i] = 0;
}

void marmot_main(void) {
  rt_gc_add_roots(hidden_roots, hidden_roots + 2);
  setup();
  clobber_stack();
  for (int round = 0; round < 6; round++) {
    rt_gc_collect();
    for (int i = 0; i < 20; i++) rt_gosched();  // let the finalizer goroutine run
  }
  CHECK(finalized[1] == 1);
  CHECK(finalized[2] == 0);
  CHECK(finalized[3] == 0);
  CHECK(finalized[4] == 1);
  CHECK(finalized[5] == 0);
  CHECK(finalized[6] == 0);
  CHECK(finalized[7] == 0);
  CHECK(finalized[8] == 1);
  CHECK(finalized[9] == 1);
  CHECK(((obj *)hidden_roots[0])->tag == 2);
  CHECK(((obj *)((char *)interior - 16))->tag == 3);
  CHECK(((obj *)*rt_gslot())->tag == 6);
  out("ok\n");
}

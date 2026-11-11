// Regions: zeroed bump allocation, GC roots while alive, reuse after free.
// Also rt_handoff ping-pong.
// expect-stdout: ok
// env: GOMAXPROCS=2
#include "rtest.h"

typedef struct rn { struct rn *l, *r; long *heap; long v; } rn;

static rn *build(rt_region *rg, int d, long *counter) {
  rn *n = rt_region_alloc(rg, sizeof(rn));
  CHECK(n->l == 0 && n->r == 0 && n->heap == 0 && n->v == 0);
  CHECK(((uintptr_t)n & 15) == 0);
  n->v = (*counter)++;
  n->heap = rt_alloc(16, RT_ALLOC_NOSCAN, 0, 0);  // only referenced from the region
  n->heap[0] = n->v;
  if (d > 0) {
    n->l = build(rg, d - 1, counter);
    n->r = build(rg, d - 1, counter);
  }
  return n;
}

static long check(rn *n) {
  CHECK(n->heap[0] == n->v);
  return 1 + (n->l ? check(n->l) + check(n->r) : 0);
}

// handoff ping-pong
static rt_mutex mu;
static rt_g *peer;
static long hits;
static void unlock_mu(void *p) { rt_unlock(&mu); }
static void ponger(void *arg) {
  for (;;) {
    rt_lock(&mu);
    rt_g *p = peer;
    peer = rt_current();
    hits++;
    if (p) {
      rt_handoff(p, unlock_mu, 0);
    } else {
      rt_park(unlock_mu, 0);
    }
  }
}

void marmot_main(void) {
  for (int round = 0; round < 30; round++) {
    rt_region *rg = rt_region_new();
    long counter = 0;
    rn *root = build(rg, 12, &counter);  // 8191 nodes, several chunks
    // Garbage to force collections while the region holds the only
    // references to its heap objects.
    for (int i = 0; i < 20000; i++) rt_alloc(256, RT_ALLOC_NOSCAN, 0, 0);
    rt_gc_collect();
    CHECK(check(root) == counter);
    void *big = rt_region_alloc(rg, 200000);  // dedicated chunk
    CHECK(((char *)big)[199999] == 0);
    rt_region_free(rg);
  }
  // A noscan region of plain linked nodes survives collections.
  {
    rt_region *nr = rt_region_new_noscan();
    rn *head = 0;
    for (int i = 0; i < 100000; i++) {
      rn *x = rt_region_alloc(nr, sizeof(rn));
      x->v = i;
      x->l = head;
      head = x;
    }
    rt_gc_collect();
    long k = 99999;
    for (rn *x = head; x; x = x->l, k--) CHECK(x->v == k);
    CHECK(k == -1);
    rt_region_free(nr);
  }
  // Reset: one region reused across iterations, objects zeroed again.
  rt_region *rr = rt_region_new();
  for (int round = 0; round < 50; round++) {
    long counter = 0;
    rn *root = build(rr, 10, &counter);
    CHECK(check(root) == counter);
    rt_region_reset(rr);
  }
  rt_region_free(rr);
  // handoff: main and ponger alternate.
  rt_go(ponger, 0);
  rt_sleep(1000000);
  for (int i = 0; i < 100000; i++) {
    rt_lock(&mu);
    rt_g *p = peer;
    peer = rt_current();
    CHECK(p != 0);
    rt_handoff(p, unlock_mu, 0);
  }
  CHECK(hits >= 100000);
  out("ok\n");
}

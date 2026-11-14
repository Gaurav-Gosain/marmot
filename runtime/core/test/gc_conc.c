// Marking beside the program (MARMOT_GC_CONC=1): goroutines rewire a shared
// graph while cycles run, every pointer store to the heap or a global goes
// through rt_wb, and pointers move between heap slots through the stack
// across allocations. MARMOT_GC_VERIFY=1 marks again at every cycle and
// stops the program on any object the concurrent mark missed.
// expect-stdout: ok
// env: MARMOT_GC_CONC=1 MARMOT_GC_VERIFY=1 GOMAXPROCS=4 GOGC=25
// tsan: skip (the mark reads objects that goroutines write, by design)
// timeout: 180
// wasm: skip (threads)
#include "gcgraph.h"

extern uint64_t gc_conc_cycles;
extern uint64_t gc_verify_missed;

#define NSHARED 256
static node *shared[NSHARED];  // global: stores go through the barrier
static waitgroup wg;

static inline void st(void *slot, void *v) {
  rt_wb((void **)slot, v);
  *(void **)slot = v;
}

static void worker(void *arg) {
  uint64_t s = (uintptr_t)arg | 1;
#define R() (s ^= s << 13, s ^= s >> 7, s ^= s << 17, s)
  node **roots = rt_alloc(sizeof(node *) * 500, RT_ALLOC_CONSERVATIVE, 0, 0);
  for (int round = 0; round < 30; round++) {
    for (int k = 0; k < 3000; k++) {
      uint64_t r = R();
      int i = (int)(r % 500), j = (int)((r >> 32) % NSHARED);
      switch ((r >> 20) % 7) {
        case 0:
        case 1:
          st(&roots[i], node_new((uintptr_t)(round * 100000 + k + 1), R()));
          break;
        case 2: {
          node *a = roots[i], *b = roots[(r >> 40) % 500];
          if (a) st(&a->edge[(r >> 50) % NE], b);
          break;
        }
        case 3:
          st(&roots[i], NULL);
          break;
        case 4: {  // take a shared node, hold it on the stack over allocations
          node *t = __atomic_exchange_n(&shared[j], NULL, __ATOMIC_ACQ_REL);
          if (t) rt_wb((void **)&t, NULL);  // the exchange removed it from the global: shade it
          for (int q = 0; q < 20; q++) node_new(1, R());
          st(&roots[i], t);
          break;
        }
        case 5: {  // give one away
          node *t = roots[i];
          rt_wb((void **)&shared[j], t);
          node *old = __atomic_exchange_n(&shared[j], t, __ATOMIC_ACQ_REL);
          (void)old;
          st(&roots[i], NULL);
          break;
        }
        case 6: {  // an edge of a node to a shared one
          node *a = roots[i], *b = __atomic_load_n(&shared[j], __ATOMIC_ACQUIRE);
          if (a) st(&a->edge[(r >> 50) % NE], b);
          break;
        }
      }
    }
    for (int i = 0; i < 500; i++) {
      node *n = roots[i];
      for (int d = 0; n && d < 30; d++) {
        node_check(n);
        n = n->edge[(n->id + (uintptr_t)d) % NE];
      }
    }
    for (int j = 0; j < NSHARED; j++) {
      node *n = __atomic_load_n(&shared[j], __ATOMIC_ACQUIRE);
      if (n) node_check(n);
    }
    if (round % 10 == 0) rt_gosched();
  }
  wg_done(&wg);
}

void marmot_main(void) {
  wg_add(&wg, 6);
  for (uintptr_t i = 0; i < 6; i++) rt_go(worker, (void *)(i * 7919 + 1));
  wg_wait(&wg);
  CHECK(gc_conc_cycles > 3);
  CHECK(gc_verify_missed == 0);
  out("ok\n");
}

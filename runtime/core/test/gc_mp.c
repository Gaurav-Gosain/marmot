// Graph stress on several goroutines and threads at once, with tight loops
// that must be stopped by signal.
// expect-stdout: ok
// tsan: skip (ThreadSanitizer defers async signals until a syscall, so tight loops never stop)
// env: GOMAXPROCS=4
// timeout: 180
// wasm: skip (needs preemption by signal)
#include "gcgraph.h"

static waitgroup wg;
static volatile int stop;

// Each worker has its own rng (gcgraph's is global): run a private copy.
static void worker(void *arg) {
  uintptr_t seed = (uintptr_t)arg;
  uint64_t st = seed | 1;
  node **roots = rt_alloc(sizeof(node *) * 500, RT_ALLOC_CONSERVATIVE, 0, 0);
  for (int round = 0; round < 40; round++) {
    for (int k = 0; k < 3000; k++) {
      st ^= st << 13; st ^= st >> 7; st ^= st << 17;
      uint64_t r = st;
      int i = (int)(r % 500);
      switch ((r >> 20) % 4) {
        case 0: case 1: {
          st ^= st << 13; st ^= st >> 7; st ^= st << 17;
          roots[i] = node_new((uintptr_t)(round * 100000 + k + 1), st);
          break;
        }
        case 2: {
          node *a = roots[i], *b = roots[(r >> 32) % 500];
          if (a) a->edge[(r >> 40) % NE] = b;
          break;
        }
        case 3: roots[i] = 0; break;
      }
    }
    for (int i = 0; i < 500; i++) {
      node *n = roots[i];
      for (int d = 0; n && d < 30; d++) {
        node_check(n);
        n = n->edge[(n->id + (uintptr_t)d) % NE];
      }
    }
    if (round % 10 == 0) rt_gosched();
  }
  wg_done(&wg);
}

static volatile long spin_count;
static void spinner(void *arg) {
  while (!stop) spin_count++;
}

void marmot_main(void) {
  rt_go(spinner, 0);
  wg_add(&wg, 6);
  for (uintptr_t i = 0; i < 6; i++) rt_go(worker, (void *)(i * 7919 + 1));
  wg_wait(&wg);
  stop = 1;
  rt_memstats st;
  rt_gc_stats(&st);
  CHECK(st.num_gc > 3);
  out("ok\n");
}

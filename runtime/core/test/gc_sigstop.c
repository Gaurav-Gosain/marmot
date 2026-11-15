// A concurrent cycle does not start marking while a thread is stopped by
// signal between calls (compiled code may hold rt_gc_marking in a register
// there): a goroutine spinning in a loop without calls is stopped by SIGURG
// at every first stop, so each cycle retries the stop and then marks with
// the world stopped. The cycles still complete, and once the spinner ends,
// cycles mark beside the program again.
// expect-stdout: ok
// env: MARMOT_GC_CONC=1 MARMOT_GC_VERIFY=1 GOMAXPROCS=4 GOGC=25
// tsan: skip (the mark reads what the allocator writes, by design)
// wasm: skip (threads, signals)
#include "gcgraph.h"

extern uint64_t gc_conc_cycles;
extern uint64_t gc_verify_missed;
extern uint64_t gc_sigstop_retries, gc_sigstop_fallbacks;

static volatile int spin_stop;
static volatile int spinning;
static waitgroup wg;

static void spinner(void *arg) {
  (void)arg;
  spinning = 1;
  while (!spin_stop) __asm__ volatile("" ::: "memory");  // no call
  wg_done(&wg);
}

static void churn(int rounds) {
  for (int r = 0; r < rounds; r++) {
    void *keep[64];
    for (int i = 0; i < 64; i++) keep[i] = rt_alloc(4096, RT_ALLOC_CONSERVATIVE, 0, 0);
    __asm__ volatile("" ::"r"(keep) : "memory");
  }
}

void marmot_main(void) {
  wg_add(&wg, 1);
  rt_go(spinner, NULL);
  while (!spinning) rt_gosched();
  uint64_t gc0 = 0;
  rt_memstats st;
  rt_gc_stats(&st);
  gc0 = st.num_gc;
  churn(400);
  rt_gc_stats(&st);
  CHECK(st.num_gc > gc0 + 3);
  CHECK(gc_sigstop_fallbacks > 0);
  CHECK(gc_sigstop_retries >= 4 * gc_sigstop_fallbacks);
  uint64_t conc0 = gc_conc_cycles;
  spin_stop = 1;
  wg_wait(&wg);
  churn(400);
  CHECK(gc_conc_cycles > conc0);
  CHECK(gc_verify_missed == 0);
  out("ok\n");
}

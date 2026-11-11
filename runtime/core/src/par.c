// par.c: fork-join parallel-for on the goroutine scheduler.
//
// rt_parallel_for splits [0, n) into chunks of `grain` iterations claimed
// dynamically with one atomic add each. The calling goroutine works too and
// returns only after every chunk has finished, so state on its stack stays
// valid for the helpers. Helpers are ordinary goroutines: they run on idle
// Ps, are visible to the GC and can be nested.

#include "internal.h"

typedef struct pfor {
  void (*body)(void *ctx, int64_t lo, int64_t hi);
  void *ctx;
  int64_t n, grain;
  _Atomic int64_t next;
  _Atomic int32_t pending;  // helpers still running
  rt_mutex mu;
  rt_g *waiter;
} pfor;

static void pfor_run(pfor *pf) {
  for (;;) {
    int64_t lo = atomic_fetch_add_explicit(&pf->next, pf->grain, memory_order_relaxed);
    if (lo >= pf->n) return;
    int64_t hi = lo + pf->grain;
    if (hi > pf->n) hi = pf->n;
    pf->body(pf->ctx, lo, hi);
  }
}

static void pfor_helper(void *arg) {
  pfor *pf = arg;
  pfor_run(pf);
  // Decrement under the lock: once the caller sees zero it returns and pf
  // (on its stack) is gone, so nothing may touch pf after the unlock.
  rt_lock(&pf->mu);
  rt_g *g = NULL;
  if (atomic_fetch_sub(&pf->pending, 1) == 1) {
    g = pf->waiter;
    pf->waiter = NULL;
  }
  rt_unlock(&pf->mu);
  if (g) rt_ready(g);
}

static void pfor_unlock(void *mu) { rt_unlock(mu); }

RT_API void rt_parallel_for(int64_t n, int64_t grain,
                            void (*body)(void *ctx, int64_t lo, int64_t hi), void *ctx) {
  if (n <= 0) return;
  int32_t procs = atomic_load(&sched.nprocs);
  if (grain <= 0) {
    // About 4 chunks per processor balances uneven iterations.
    grain = n / ((int64_t)procs * 4);
    if (grain < 1) grain = 1;
  }
  int64_t chunks = (n + grain - 1) / grain;
  int64_t helpers = chunks - 1;
  if (helpers > procs - 1) helpers = procs - 1;
  if (helpers <= 0) {
    body(ctx, 0, n);
    return;
  }
  pfor pf;
  pf.body = body;
  pf.ctx = ctx;
  pf.n = n;
  pf.grain = grain;
  atomic_store(&pf.next, 0);
  atomic_store(&pf.pending, (int32_t)helpers);
  pf.mu.state = 0;
  pf.waiter = NULL;
  for (int64_t i = 0; i < helpers; i++) rt_go(pfor_helper, &pf);
  pfor_run(&pf);
  rt_lock(&pf.mu);
  if (atomic_load(&pf.pending) == 0) {
    rt_unlock(&pf.mu);
    return;
  }
  pf.waiter = rt_current();
  rt_park(pfor_unlock, &pf.mu);
}

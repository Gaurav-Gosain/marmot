// rt_parallel_for: overhead of a call and speedup on a compute loop.
#include "rt.h"
#include <stdio.h>

static double *a;
static void work(void *ctx, int64_t lo, int64_t hi) {
  for (int64_t i = lo; i < hi; i++) {
    double x = (double)i;
    for (int k = 0; k < 50; k++) x = x * 0.999999 + 1.0;
    a[i] = x;
  }
}
static void nop(void *ctx, int64_t lo, int64_t hi) {}

void marmot_main(void) {
  long n = 4000000;
  a = rt_alloc((uintptr_t)n * 8, RT_ALLOC_NOSCAN, 0, 0);
  work(0, 0, n);  // fault the pages in first
  int64_t t0 = rt_nanotime();
  work(0, 0, n);
  int64_t t1 = rt_nanotime();
  rt_parallel_for(n, 0, work, 0);
  int64_t t2 = rt_nanotime();
  printf("compute loop: sequential %.1f ms, parallel %.1f ms on %d procs (%.1fx)\n", (t1 - t0) / 1e6,
         (t2 - t1) / 1e6, rt_gomaxprocs(0), (double)(t1 - t0) / (double)(t2 - t1));
  // Short loops repeated 2000 times, at three sizes.
  long sizes[3] = {400, 2000, 20000};
  for (int z = 0; z < 3; z++) {
    long sn = sizes[z];
    int64_t s0 = rt_nanotime();
    for (int r = 0; r < 2000; r++) work(0, 0, sn);
    int64_t s1 = rt_nanotime();
    for (int r = 0; r < 2000; r++) rt_parallel_for(sn, 0, work, 0);
    int64_t s2 = rt_nanotime();
    printf("short loops (%.1f us each): sequential %.1f ms, parallel %.1f ms (%.1fx)\n",
           (s1 - s0) / 2000.0 / 1e3, (s1 - s0) / 1e6, (s2 - s1) / 1e6, (double)(s1 - s0) / (double)(s2 - s1));
  }
  int reps = 20000;
  t0 = rt_nanotime();
  for (int r = 0; r < reps; r++) rt_parallel_for(1024, 64, nop, 0);
  t1 = rt_nanotime();
  printf("parallel_for call overhead (16 chunks, empty body): %.2f us\n", (t1 - t0) / 1e3 / reps);
  fflush(stdout);
}

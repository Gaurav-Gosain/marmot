// rt_parallel_for: coverage, nesting, many calls, GC during the loop.
// expect-stdout: ok
// env: GOMAXPROCS=4
#include "rtest.h"

typedef struct { _Atomic int *hits; long n; } cov;
static void mark(void *ctx, int64_t lo, int64_t hi) {
  cov *c = ctx;
  CHECK(lo >= 0 && hi <= c->n && lo < hi);
  for (int64_t i = lo; i < hi; i++) c->hits[i]++;
}

static _Atomic long nested_sum;
static void inner(void *ctx, int64_t lo, int64_t hi) {
  for (int64_t i = lo; i < hi; i++) nested_sum += i;
}
static void outer(void *ctx, int64_t lo, int64_t hi) {
  for (int64_t i = lo; i < hi; i++) rt_parallel_for(100, 7, inner, 0);
}

static void allocating(void *ctx, int64_t lo, int64_t hi) {
  for (int64_t i = lo; i < hi; i++) {
    long *p = rt_alloc(64, RT_ALLOC_NOSCAN, 0, 0);
    p[0] = i;
    ((long **)ctx)[i] = p;
  }
}

void marmot_main(void) {
  long sizes[] = {0, 1, 2, 3, 7, 100, 1000, 100003};
  for (int k = 0; k < 8; k++) {
    long n = sizes[k];
    _Atomic int *h = rt_alloc((uintptr_t)(n ? n : 1) * sizeof(int), RT_ALLOC_NOSCAN, 0, 0);
    cov c = {h, n};
    long grains[] = {0, 1, 13, 1000000};
    for (int g = 0; g < 4; g++) {
      rt_parallel_for(n, grains[g], mark, &c);
      for (long i = 0; i < n; i++) CHECK(h[i] == g + 1);
    }
  }
  rt_parallel_for(50, 1, outer, 0);
  CHECK(nested_sum == 50L * (99 * 100 / 2));
  for (int r = 0; r < 2000; r++) rt_parallel_for(64, 1, inner, 0);
  long **objs = rt_alloc(200000 * sizeof(long *), RT_ALLOC_CONSERVATIVE, 0, 0);
  rt_parallel_for(200000, 0, allocating, objs);
  rt_gc_collect();
  for (long i = 0; i < 200000; i++) CHECK(objs[i][0] == i);
  for (int i = 0; i < 1000 && rt_numgoroutine() != 1; i++) rt_sleep(1000000);
  CHECK(rt_numgoroutine() == 1);
  // rt_ranges_disjoint against a quadratic check.
  uint64_t rs = 12345;
  for (int iter = 0; iter < 3000; iter++) {
    uintptr_t lo[64], hi[64], lo2[64], hi2[64];
    int64_t n = (int64_t)(iter % 64);
    for (int64_t i = 0; i < n; i++) {
      rs ^= rs << 13; rs ^= rs >> 7; rs ^= rs << 17;
      lo[i] = (uintptr_t)(rs % 4000);
      hi[i] = lo[i] + (uintptr_t)((rs >> 20) % (iter % 3 ? 40 : 3));
      if (iter % 5 == 0) { lo[i] = (uintptr_t)i * 100; hi[i] = lo[i] + 100 - (uintptr_t)(rs % 2); }
      lo2[i] = lo[i]; hi2[i] = hi[i];
    }
    int want = 1;
    for (int64_t i = 0; i < n; i++)
      for (int64_t j = i + 1; j < n; j++)
        if (lo2[i] < hi2[i] && lo2[j] < hi2[j] && lo2[i] < hi2[j] && lo2[j] < hi2[i]) want = 0;
    CHECK(rt_ranges_disjoint(n, lo, hi) == want);
  }
  out("ok\n");
}

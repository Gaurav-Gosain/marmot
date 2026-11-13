// rt_sort_*: against qsort on many sizes and distributions.
// expect-stdout: ok
// env: GOMAXPROCS=4
// wasm: skip (needs libc headers)
#include "rtest.h"
#include <math.h>
#include <stdlib.h>

static int cmp_i64(const void *a, const void *b) {
  int64_t x = *(const int64_t *)a, y = *(const int64_t *)b;
  return x < y ? -1 : x > y;
}
static int cmp_f64(const void *a, const void *b) {
  double x = *(const double *)a, y = *(const double *)b;
  int xn = x != x, yn = y != y;
  if (xn || yn) return yn - xn;  // NaNs first
  return x < y ? -1 : x > y;
}

static uint64_t rs = 42;
static uint64_t rnd(void) { rs ^= rs << 13; rs ^= rs >> 7; rs ^= rs << 17; return rs; }

void marmot_main(void) {
  intptr_t sizes[] = {0, 1, 2, 3, 31, 32, 33, 100, 4095, 4096, 5000, 70000, 1100000};
  for (int si = 0; si < 13; si++) {
    intptr_t n = sizes[si];
    int64_t *a = rt_alloc((uintptr_t)(n + 1) * 8, RT_ALLOC_NOSCAN, 0, 0);
    int64_t *b = malloc((size_t)(n + 1) * 8);
    for (int dist = 0; dist < 4; dist++) {
      for (intptr_t i = 0; i < n; i++) {
        uint64_t r = rnd();
        a[i] = dist == 0 ? (int64_t)r : dist == 1 ? (int64_t)(r % 10) - 5 : dist == 2 ? (int64_t)(r >> 33) : (int64_t)i;
      }
      memcpy(b, a, (size_t)n * 8);
      rt_sort_i64(a, n);
      qsort(b, (size_t)n, 8, cmp_i64);
      CHECK(memcmp(a, b, (size_t)n * 8) == 0);
    }
    double *f = (double *)a, *g = (double *)b;
    for (intptr_t i = 0; i < n; i++) {
      uint64_t r = rnd();
      f[i] = r % 17 == 0 ? NAN : r % 13 == 0 ? (r & 1 ? -0.0 : 0.0) : r % 11 == 0 ? INFINITY * (r & 2 ? 1 : -1)
                                                                    : ((double)(int64_t)r) / 1e9;
    }
    memcpy(g, f, (size_t)n * 8);
    rt_sort_f64(f, n);
    qsort(g, (size_t)n, 8, cmp_f64);
    for (intptr_t i = 0; i < n; i++) CHECK((f[i] != f[i] && g[i] != g[i]) || f[i] == g[i]);
    free(b);
  }
  out("ok\n");
}

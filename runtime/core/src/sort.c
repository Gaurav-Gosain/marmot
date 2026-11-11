// sort.c: sorting of plain number slices for sort.Ints, slices.Sort and
// friends. Results equal Go's (for floats: NaNs first, as Float64s and
// slices.Sort order them; -0 and +0 are equal and may come out in either
// order, which no comparison can observe).
//
// Small inputs use insertion sort; large ones an LSD radix sort on 8-bit
// digits that skips digits where every key agrees, with a temporary buffer
// mapped from the OS (not GC memory). Large sorts split the counting and
// scattering of each pass across processors with rt_parallel_for.

#include "internal.h"

INLINE void insertion_u64(uint64_t *a, intptr_t n) {
  for (intptr_t i = 1; i < n; i++) {
    uint64_t v = a[i];
    intptr_t j = i - 1;
    while (j >= 0 && a[j] > v) {
      a[j + 1] = a[j];
      j--;
    }
    a[j + 1] = v;
  }
}

// Quicksort with median of three for mid-size inputs.
static void quick_u64(uint64_t *a, intptr_t n) {
  while (n > 32) {
    uint64_t x = a[0], y = a[n / 2], z = a[n - 1];
    uint64_t p = x < y ? (y < z ? y : (x < z ? z : x)) : (x < z ? x : (y < z ? z : y));
    intptr_t i = 0, j = n - 1;
    for (;;) {
      while (a[i] < p) i++;
      while (a[j] > p) j--;
      if (i >= j) break;
      uint64_t t = a[i];
      a[i] = a[j];
      a[j] = t;
      i++;
      j--;
    }
    // Recurse into the smaller side.
    if (j + 1 < n - j - 1) {
      quick_u64(a, j + 1);
      a += j + 1;
      n -= j + 1;
    } else {
      quick_u64(a + j + 1, n - j - 1);
      n = j + 1;
    }
  }
  insertion_u64(a, n);
}

#define RADIX_MIN 4096
#define PAR_MIN (1 << 20)
#define MAX_PARTS 64

typedef struct {
  const uint64_t *src;
  uint64_t *dst;
  intptr_t n, parts, chunk;
  int shift;
  uint32_t (*cnt)[256];  // per part counts, then start offsets
} radix_pass;

static void count_part(void *ctx, int64_t lo, int64_t hi) {
  radix_pass *rp = ctx;
  for (int64_t part = lo; part < hi; part++) {
    uint32_t *c = rp->cnt[part];
    __builtin_memset(c, 0, 256 * sizeof(uint32_t));
    intptr_t b = part * rp->chunk, e = b + rp->chunk < rp->n ? b + rp->chunk : rp->n;
    for (intptr_t i = b; i < e; i++) c[(rp->src[i] >> rp->shift) & 0xFF]++;
  }
}

static void scatter_part(void *ctx, int64_t lo, int64_t hi) {
  radix_pass *rp = ctx;
  for (int64_t part = lo; part < hi; part++) {
    uint32_t *off = rp->cnt[part];
    intptr_t b = part * rp->chunk, e = b + rp->chunk < rp->n ? b + rp->chunk : rp->n;
    for (intptr_t i = b; i < e; i++) {
      uint64_t v = rp->src[i];
      rp->dst[off[(v >> rp->shift) & 0xFF]++] = v;
    }
  }
}

// Sort unsigned 64-bit keys ascending.
static void radix_u64(uint64_t *a, intptr_t n) {
  // Which digits differ at all?
  uint64_t and_all = ~0ull, or_all = 0;
  for (intptr_t i = 0; i < n; i++) {
    and_all &= a[i];
    or_all |= a[i];
  }
  uint64_t differ = and_all ^ or_all;
  if (!differ) return;
  uintptr_t bytes = ((uintptr_t)n * 8 + rt_pagesize - 1) & ~(rt_pagesize - 1);
  uint64_t *tmp = os_map(bytes);
  if (!tmp) {
    quick_u64(a, n);
    return;
  }
  intptr_t parts = 1;
  if (n >= PAR_MIN) {
    parts = atomic_load(&sched.nprocs);
    if (parts > MAX_PARTS) parts = MAX_PARTS;
    if (parts < 1) parts = 1;
  }
  uint32_t(*cnt)[256] = os_map((uintptr_t)parts * 256 * sizeof(uint32_t));
  if (!cnt) {
    os_unmap(tmp, bytes);
    quick_u64(a, n);
    return;
  }
  uint64_t *src = a, *dst = tmp;
  for (int shift = 0; shift < 64; shift += 8) {
    if (!((differ >> shift) & 0xFF)) continue;
    radix_pass rp = {src, dst, n, parts, (n + parts - 1) / parts, shift, cnt};
    if (parts > 1)
      rt_parallel_for(parts, 1, count_part, &rp);
    else
      count_part(&rp, 0, 1);
    // Exclusive prefix over (digit, part) so each part writes its own range.
    uint32_t sum = 0;
    for (int d = 0; d < 256; d++)
      for (intptr_t p = 0; p < parts; p++) {
        uint32_t c = cnt[p][d];
        cnt[p][d] = sum;
        sum += c;
      }
    if (parts > 1)
      rt_parallel_for(parts, 1, scatter_part, &rp);
    else
      scatter_part(&rp, 0, 1);
    uint64_t *t = src;
    src = dst;
    dst = t;
  }
  if (src != a) __builtin_memcpy(a, src, (size_t)n * 8);
  os_unmap(cnt, (uintptr_t)parts * 256 * sizeof(uint32_t));
  os_unmap(tmp, bytes);
}

static void sort_u64(uint64_t *a, intptr_t n) {
  if (n <= 32)
    insertion_u64(a, n);
  else if (n < RADIX_MIN)
    quick_u64(a, n);
  else
    radix_u64(a, n);
}

RT_API void rt_sort_u64(uint64_t *a, intptr_t n) { sort_u64(a, n); }

// Signed: flip the sign bit so unsigned order matches.
RT_API void rt_sort_i64(int64_t *a, intptr_t n) {
  uint64_t *u = (uint64_t *)a;
  for (intptr_t i = 0; i < n; i++) u[i] ^= 1ull << 63;
  sort_u64(u, n);
  for (intptr_t i = 0; i < n; i++) u[i] ^= 1ull << 63;
}

// Floats: NaNs first, then the order of the values. Map each float to an
// unsigned key whose order matches (negatives: invert all bits; positives:
// set the sign bit), after moving NaNs to the front.
RT_API void rt_sort_f64(double *a, intptr_t n) {
  uint64_t *u = (uint64_t *)a;
  intptr_t nan = 0;
  for (intptr_t i = 0; i < n; i++) {
    if (a[i] != a[i]) {
      uint64_t t = u[nan];
      u[nan] = u[i];
      u[i] = t;
      nan++;
    }
  }
  uint64_t *v = u + nan;
  intptr_t m = n - nan;
  for (intptr_t i = 0; i < m; i++) v[i] = (v[i] >> 63) ? ~v[i] : v[i] | (1ull << 63);
  sort_u64(v, m);
  for (intptr_t i = 0; i < m; i++) v[i] = (v[i] >> 63) ? v[i] & ~(1ull << 63) : ~v[i];
}

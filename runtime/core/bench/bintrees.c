// binary-trees on the runtime core, same shape as bench/programs/bintrees.
#include "rt.h"
#include <stdio.h>
#include <sys/resource.h>

static double cpu_s(void) {
  struct rusage ru;
  getrusage(RUSAGE_SELF, &ru);
  return ru.ru_utime.tv_sec + ru.ru_stime.tv_sec + (ru.ru_utime.tv_usec + ru.ru_stime.tv_usec) / 1e6;
}

typedef struct node { struct node *l, *r; } node;
static const uint8_t node_map[1] = {0x3};

static node *build(int d) {
  node *n = rt_alloc(sizeof(node), RT_ALLOC_PRECISE, node_map, sizeof(node));
  if (d > 0) {
    n->l = build(d - 1);
    n->r = build(d - 1);
  }
  return n;
}

static long check(node *n) { return n->l ? 1 + check(n->l) + check(n->r) : 1; }

void marmot_main(void) {
  int max = 19;
  int64_t t0 = rt_nanotime();
  double c0 = cpu_s();
  node *lng = build(max);
  long total = 0;
  for (int d = 4; d <= max; d += 4) {
    int iters = 1 << (max - d + 4);
    long c = 0;
    for (int i = 0; i < iters; i++) c += check(build(d));
    total += c;
  }
  int64_t t1 = rt_nanotime();
  rt_memstats st;
  rt_gc_stats(&st);
  printf("bintrees: %.3f s wall, %.3f s cpu, %lld GCs, pause total %.1f ms, check %ld %ld\n", (t1 - t0) / 1e9, cpu_s() - c0,
         (long long)st.num_gc, st.pause_total_ns / 1e6, check(lng), total);
  fflush(stdout);
}

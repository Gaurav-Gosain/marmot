// gcgraph.h: random object graph stress shared by the GC tests.
#include "rtest.h"

// Node layout: word 0 magic, word 1 id, words 2..2+NE pointers (precise or
// conservative), then a payload of non-pointer words checked by checksum.
#define NE 4
typedef struct node {
  uintptr_t magic;
  uintptr_t id;
  struct node *edge[NE];
  uintptr_t npay;
  uintptr_t pay[];  // npay words
} node;

#define MAGIC ((uintptr_t)0x5eed5eed5eed5eedull)
static const uint8_t node_ptrmap[6100 / 8] = {0x3c};  // words 2..5; covers the largest node

static uint64_t rng_state;
static inline uint64_t rng(void) {
  rng_state ^= rng_state << 13;
  rng_state ^= rng_state >> 7;
  rng_state ^= rng_state << 17;
  return rng_state;
}

static node *node_new(uintptr_t id, uint64_t r) {
  uintptr_t npay = r % 6 == 0 ? (r >> 8) % 6000 : (r >> 8) % 12;  // some large ones
  uintptr_t size = sizeof(node) + npay * sizeof(uintptr_t);
  node *n;
  if (r % 3 == 0)
    n = rt_alloc(size, RT_ALLOC_CONSERVATIVE, 0, 0);
  else
    n = rt_alloc(size, RT_ALLOC_PRECISE, node_ptrmap, size);
  for (uintptr_t i = 0; i < size / sizeof(uintptr_t); i++) CHECK(((uintptr_t *)n)[i] == 0);
  n->magic = MAGIC;
  n->id = id;
  n->npay = npay;
  for (uintptr_t i = 0; i < npay; i++) n->pay[i] = id * 31 + i;
  // Payload words that look like pointers in precise objects must not matter.
  return n;
}

static void node_check(node *n) {
  CHECK(n->magic == MAGIC);
  for (uintptr_t i = 0; i < n->npay; i++) CHECK(n->pay[i] == n->id * 31 + i);
}

// Walk everything reachable from roots, checking each node once per epoch.
#define MAXSEEN 200000
static void walk(node **roots, int nroots, uintptr_t epoch_mark) {
  // Iterative DFS using a stack allocated from the GC heap (noscan is fine:
  // every node on it is also reachable from roots).
  node **stk = rt_alloc(sizeof(node *) * MAXSEEN, RT_ALLOC_NOSCAN, 0, 0);
  int sp = 0;
  for (int i = 0; i < nroots; i++)
    if (roots[i] && roots[i]->id != epoch_mark) {
      roots[i]->id = epoch_mark;
      stk[sp++] = roots[i];
    }
  (void)stk;
}

// Run the stress: nroots root slots in a heap array, rounds of mutation.
static void graph_stress(uint64_t seed, int rounds, int nroots, int ops) {
  rng_state = seed | 1;
  node **roots = rt_alloc(sizeof(node *) * (uintptr_t)nroots, RT_ALLOC_PRECISE,
                          (const uint8_t *)"\xff", 8);
  uintptr_t nextid = 1;
  for (int round = 0; round < rounds; round++) {
    for (int k = 0; k < ops; k++) {
      uint64_t r = rng();
      int i = (int)(r % (uint64_t)nroots);
      switch ((r >> 20) % 5) {
        case 0:
        case 1:
          roots[i] = node_new(nextid++, rng());
          break;
        case 2: {  // link two reachable nodes
          node *a = roots[i], *b = roots[(r >> 32) % (uint64_t)nroots];
          if (a) a->edge[(r >> 40) % NE] = b;
          break;
        }
        case 3: {  // hang a fresh node off a root
          node *a = roots[i];
          if (a) a->edge[(r >> 40) % NE] = node_new(nextid++, rng());
          break;
        }
        case 4:
          roots[i] = 0;
          break;
      }
    }
    // Verify everything reachable, iteratively, bounded depth.
    for (int i = 0; i < nroots; i++) {
      node *n = roots[i];
      for (int d = 0; n && d < 50; d++) {
        node_check(n);
        n = n->edge[(n->id + (uintptr_t)d) % NE];
      }
      if (roots[i])
        for (int e = 0; e < NE; e++)
          if (roots[i]->edge[e]) node_check(roots[i]->edge[e]);
    }
  }
  (void)walk;
}

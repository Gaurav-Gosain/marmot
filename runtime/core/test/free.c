// rt_free: immediate reuse of small objects, large pages returned, garbage
// collections in between, non-heap pointers ignored.
// expect-stdout: ok
// env: GOMAXPROCS=2
#include "rtest.h"

typedef struct cell { struct cell *next; long v; long pad[2]; } cell;
static const uint8_t cell_map[1] = {0x1};

void marmot_main(void) {
  // Reuse: the next allocation of the same class and kind gets the object.
  void *a = rt_alloc(48, RT_ALLOC_NOSCAN, 0, 0);
  memset(a, 0x5a, 48);
  rt_free(a, 48);
  void *b = rt_alloc(48, RT_ALLOC_NOSCAN, 0, 0);
  CHECK(a == b);
  for (int i = 0; i < 48; i++) CHECK(((unsigned char *)b)[i] == 0);
  // Different kind does not take it.
  rt_free(b, 48);
  void *c = rt_alloc(48, RT_ALLOC_CONSERVATIVE, 0, 0);
  CHECK(c != b);
  // Ignored pointers.
  static char global[64];
  char local[64];
  rt_free(global, 64);
  rt_free(local, 64);
  rt_free((char *)c + 8, 40);  // interior
  rt_free(0, 0);
  // Precise objects reused through the free list keep the GC exact: a list
  // whose cells are freed and reallocated must survive collections.
  cell *head = 0;
  uint64_t rs = 9;
  for (int round = 0; round < 200; round++) {
    for (int i = 0; i < 1000; i++) {
      cell *x = rt_alloc(sizeof(cell), RT_ALLOC_PRECISE, cell_map, sizeof(cell));
      CHECK(x->next == 0 && x->v == 0);
      x->v = round * 1000 + i;
      x->next = head;
      head = x;
    }
    // Free about half of the list (unlink then free).
    cell **pp = &head;
    while (*pp) {
      rs ^= rs << 13; rs ^= rs >> 7; rs ^= rs << 17;
      if (rs & 1) {
        cell *dead = *pp;
        *pp = dead->next;
        rt_free(dead, sizeof(cell));
      } else {
        pp = &(*pp)->next;
      }
    }
    if (round % 20 == 0) rt_gc_collect();
    long last = (long)(~0ul >> 1);
    for (cell *x = head; x; x = x->next) {
      CHECK(x->v < last);
      last = x->v;
    }
  }
  // Large objects give their pages back and are reused.
  rt_memstats st0, st1;
  rt_gc_stats(&st0);
  for (int i = 0; i < 200; i++) {
    char *big = rt_alloc(1 << 20, RT_ALLOC_NOSCAN, 0, 0);
    CHECK(big[12345] == 0);
    big[12345] = 1;
    rt_free(big, 1 << 20);
  }
  rt_gc_stats(&st1);
  CHECK(st1.heap_sys - st0.heap_sys < (32u << 20));
  out("ok\n");
}

// A program with one goroutine that links rt_gc_parallel_enable gets the
// parallel collector from the start (its constructor; MARMOT_GCTRACE=1
// shows 4 workers). Calling it again is harmless; timers start late.
// expect-stdout: ok
// env: GOMAXPROCS=8 MARMOT_GCPROCS=4
#include "rtest.h"

typedef struct node { struct node *next; long v; } node;

static long churn(int rounds) {
  long sum = 0;
  for (int r = 0; r < rounds; r++) {
    node *head = 0;
    for (int i = 0; i < 20000; i++) {
      node *n = rt_alloc(sizeof(node), RT_ALLOC_CONSERVATIVE, 0, 0);
      n->next = head;
      n->v = i;
      head = n;
    }
    for (node *n = head; n; n = n->next) sum += n->v;
  }
  return sum;
}

void marmot_main(void) {
  long want = 20000L * 19999 / 2;
  CHECK(churn(50) == want * 50);
  rt_gc_collect();
  rt_gc_parallel_enable();
  CHECK(churn(50) == want * 50);
  rt_gc_collect();
  rt_sleep(1000000);  // timers after the fact
  CHECK(churn(5) == want * 5);
  out("ok\n");
}

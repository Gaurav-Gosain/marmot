// M:N stress: many goroutines on several threads, cross-thread ready,
// run queue overflow, stealing.
// expect-stdout: ok
// env: GOMAXPROCS=8
// timeout: 120
// wasm: skip (threads)
#include "rtest.h"

static waitgroup wg;
static rt_mutex mu;
static long total;

static void adder(void *arg) {
  long local = 0;
  for (int i = 0; i < 100; i++) {
    local += (long)(intptr_t)arg;
    if (i % 10 == 0) rt_gosched();
  }
  rt_lock(&mu);
  total += local;
  rt_unlock(&mu);
  wg_done(&wg);
}

// A ring of goroutines passing a token with park/ready.
#define RING 64
typedef struct {
  rt_mutex mu;
  rt_g *g;
  int has;
  long val;
} slot;
static slot ring[RING];
static waitgroup ringwg;
static void slot_unlock(void *p) { rt_unlock(&((slot *)p)->mu); }

static long take(slot *s) {
  rt_lock(&s->mu);
  while (!s->has) {
    s->g = rt_current();
    rt_park(slot_unlock, s);
    rt_lock(&s->mu);
  }
  s->has = 0;
  long v = s->val;
  rt_unlock(&s->mu);
  return v;
}
static void give(slot *s, long v) {
  rt_lock(&s->mu);
  s->has = 1;
  s->val = v;
  rt_g *g = s->g;
  s->g = 0;
  rt_unlock(&s->mu);
  if (g) rt_ready(g);
}

static void ringer(void *arg) {
  intptr_t i = (intptr_t)arg;
  for (;;) {
    long v = take(&ring[i]);
    if (v < 0 || (i == 0 && v >= 200 * RING)) {
      // Stop: pass the stop value on; the last ringer leaves it in slot 0.
      give(&ring[(i + 1) % RING], -1);
      break;
    }
    give(&ring[(i + 1) % RING], v + 1);
  }
  wg_done(&ringwg);
}

void marmot_main(void) {
  CHECK(rt_gomaxprocs(0) == 8);
  for (int round = 0; round < 20; round++) {
    total = 0;
    wg_add(&wg, 1000);
    for (int i = 0; i < 1000; i++) rt_go(adder, (void *)(intptr_t)1);
    wg_wait(&wg);
    CHECK(total == 100000);
  }
  wg_add(&ringwg, RING);
  for (intptr_t i = 0; i < RING; i++) rt_go(ringer, (void *)i);
  give(&ring[0], 0);
  wg_wait(&ringwg);
  // Goroutines that called wg_done may not have exited yet.
  for (int i = 0; i < 1000 && rt_numgoroutine() != 1; i++) rt_sleep(1000000);
  CHECK(rt_numgoroutine() == 1);
  out("ok\n");
}

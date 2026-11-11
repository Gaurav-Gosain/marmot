// rt_sleep and timers.
// expect-stdout: 1 2 3 timer stopped
#include "rtest.h"

static waitgroup wg;
static rt_mutex mu;
static char order[8];
static int norder;

static void sleeper(void *arg) {
  intptr_t ms = (intptr_t)arg;
  rt_sleep(ms * 10000000);
  rt_lock(&mu);
  order[norder++] = (char)('0' + ms);
  rt_unlock(&mu);
  wg_done(&wg);
}

static int fired;
static void on_timer(void *arg) {
  fired++;
  wg_done(arg);
}

void marmot_main(void) {
  wg_add(&wg, 3);
  rt_go(sleeper, (void *)3);
  rt_go(sleeper, (void *)1);
  rt_go(sleeper, (void *)2);
  int64_t t0 = rt_nanotime();
  wg_wait(&wg);
  int64_t dt = rt_nanotime() - t0;
  CHECK(dt >= 30000000 && dt < 5000000000LL);  // loose upper bound: shared machine
  for (int i = 0; i < 3; i++) {
    char b[3] = {order[i], i < 2 ? ' ' : ' ', 0};
    out(b);
  }

  // A stopped timer never fires; a started one does.
  waitgroup w2 = {0};
  void *t = rt_timer_start(rt_nanotime() + 50000000, on_timer, &w2);
  CHECK(rt_timer_stop(t) == 1);
  CHECK(rt_timer_stop(t) == 0);
  wg_add(&w2, 1);
  t = rt_timer_start(rt_nanotime() + 5000000, on_timer, &w2);
  wg_wait(&w2);
  CHECK(fired == 1);
  CHECK(rt_timer_stop(t) == 0);
  // Many timers in random order.
  wg_add(&w2, 200);
  for (int i = 0; i < 200; i++) rt_timer_start(rt_nanotime() + ((i * 7919) % 200) * 50000, on_timer, &w2);
  wg_wait(&w2);
  CHECK(fired == 201);
  // Main sleeping alone is not a deadlock.
  rt_sleep(1000000);
  out("timer stopped\n");
}

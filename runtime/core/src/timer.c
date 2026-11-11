// timer.c: timers, rt_sleep.
//
// One global 4-ary heap ordered by deadline, under tlock. Timers from
// rt_timer_start are GC objects (the handle stays valid as long as the Go
// side holds it); their callbacks run on one system goroutine, timerproc.
// rt_sleep uses a per-goroutine timer that readies the goroutine directly
// from the scheduler, with no goroutine hop.
//
// Expired timers are run by any M in findrunnable (timers_check). An idle M
// sleeps until the next deadline (sched.timer_m); adding an earlier timer
// wakes it.

#include "internal.h"

enum { T_PENDING = 1, T_FIRED, T_STOPPED, T_DONE };

struct rt_timer {
  int64_t when;
  void (*fn)(void *);
  void *arg;
  rt_timer *next;   // fire queue
  int32_t idx;      // heap index, -1 when not in the heap
  uint8_t state;
  uint8_t sleep;    // rt_sleep timer: arg is the goroutine to ready
};

_Atomic int64_t timer_next_when = INT64_MAX;

static rt_spin tlock;
static rt_timer **theap;
static int32_t tlen, tcap;
static rt_timer *fireq_head, *fireq_tail;
static rt_g *timer_g;
static bool timer_g_parked;
static rt_spin timer_g_lock;

void timer_init(void) {}

static void hswap(int32_t i, int32_t j) {
  rt_timer *t = theap[i];
  theap[i] = theap[j];
  theap[j] = t;
  theap[i]->idx = i;
  theap[j]->idx = j;
}

static void siftup(int32_t i) {
  while (i > 0) {
    int32_t p = (i - 1) / 4;
    if (theap[p]->when <= theap[i]->when) break;
    hswap(i, p);
    i = p;
  }
}

static void siftdown(int32_t i) {
  for (;;) {
    int32_t c = 4 * i + 1, best = i;
    for (int32_t k = c; k < c + 4 && k < tlen; k++)
      if (theap[k]->when < theap[best]->when) best = k;
    if (best == i) return;
    hswap(i, best);
    i = best;
  }
}

static void update_next(void) {
  atomic_store(&timer_next_when, tlen ? theap[0]->when : INT64_MAX);
}

// tlock held.
static void heap_add(rt_timer *t) {
  if (tlen == tcap) {
    int32_t ncap = tcap ? tcap * 2 : 64;
    rt_timer **n = persistent_alloc((uintptr_t)ncap * sizeof *n, 8);
    if (tlen) __builtin_memcpy(n, theap, (uintptr_t)tlen * sizeof *n);
    theap = n;  // old array is leaked; it is small and growth is rare
    tcap = ncap;
  }
  t->idx = tlen;
  theap[tlen++] = t;
  siftup(t->idx);
}

static void heap_remove(rt_timer *t) {
  int32_t i = t->idx;
  int32_t last = --tlen;
  if (i != last) {
    hswap(i, last);
    siftdown(i);
    siftup(i);
  }
  t->idx = -1;
}

static void timer_add(rt_timer *t) {
  spin_lock(&tlock);
  t->state = T_PENDING;
  heap_add(t);
  bool first = theap[0] == t;
  update_next();
  spin_unlock(&tlock);
  if (first) sched_timer_added(t->when);
}

static void tlock_unlock(void *unused) { spin_unlock(&tlock); }

static void timerproc(void *unused) {
  for (;;) {
    spin_lock(&tlock);
    while (!fireq_head) {
      timer_g_parked = true;
      rt_park(tlock_unlock, NULL);
      spin_lock(&tlock);
    }
    rt_timer *t = fireq_head;
    fireq_head = t->next;
    if (!fireq_head) fireq_tail = NULL;
    t->next = NULL;
    t->state = T_DONE;
    void (*fn)(void *) = t->fn;
    void *arg = t->arg;
    spin_unlock(&tlock);
    fn(arg);
  }
}

int64_t timers_check(int64_t now) {
  rt_timer *ready = NULL;
  bool wake = false;
  spin_lock(&tlock);
  while (tlen > 0 && theap[0]->when <= now) {
    rt_timer *t = theap[0];
    heap_remove(t);
    if (t->sleep) {
      t->state = T_DONE;
      t->next = ready;
      ready = t;
    } else {
      t->state = T_FIRED;
      t->next = NULL;
      if (fireq_tail)
        fireq_tail->next = t;
      else
        fireq_head = t;
      fireq_tail = t;
      if (timer_g_parked) {
        timer_g_parked = false;
        wake = true;
      }
    }
  }
  update_next();
  int64_t next = tlen ? theap[0]->when : -1;
  spin_unlock(&tlock);
  while (ready) {
    rt_timer *t = ready;
    ready = t->next;
    t->next = NULL;
    rt_ready((rt_g *)t->arg);
  }
  if (wake) rt_ready(timer_g);
  return next;
}

int64_t timers_next(void) {
  int64_t w = atomic_load(&timer_next_when);
  return w == INT64_MAX ? -1 : w;
}

RT_API void *rt_timer_start(int64_t when, void (*fn)(void *), void *arg) {
  if (!timer_g) {
    spin_lock(&timer_g_lock);
    if (!timer_g) {
      // timerproc parks right away; mark it parked once it has.
      rt_g *g = new_system_g(timerproc, NULL);
      __atomic_store_n(&timer_g, g, __ATOMIC_RELEASE);
    }
    spin_unlock(&timer_g_lock);
  }
  rt_timer *t = rt_alloc(sizeof *t, RT_ALLOC_CONSERVATIVE, NULL, 0);
  t->when = when;
  t->fn = fn;
  t->arg = arg;
  t->idx = -1;
  timer_add(t);
  return t;
}

RT_API int32_t rt_timer_stop(void *handle) {
  rt_timer *t = handle;
  int32_t r = 0;
  spin_lock(&tlock);
  if (t->state == T_PENDING) {
    heap_remove(t);
    update_next();
    t->state = T_STOPPED;
    r = 1;
  }
  spin_unlock(&tlock);
  return r;
}

static void sleep_park(void *gp) {
  rt_g *g = gp;
  timer_add(g->sleeptimer);
}

RT_API void rt_sleep(int64_t ns) {
  if (ns <= 0) return;
  rt_m *m = getm();
  rt_g *g = m->curg;
  if (!g->sleeptimer) {
    g->sleeptimer = persistent_alloc(sizeof(rt_timer), 8);
    g->sleeptimer->sleep = 1;
    g->sleeptimer->arg = g;
  }
  g->sleeptimer->when = os_nanotime() + ns;
  rt_park(sleep_park, g);
}

void timers_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi)) {
  if (tlen) scan((uintptr_t)theap, (uintptr_t)(theap + tlen));
  if (fireq_head) scan((uintptr_t)&fireq_head, (uintptr_t)(&fireq_head + 1));
}

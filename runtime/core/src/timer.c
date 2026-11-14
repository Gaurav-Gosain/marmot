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
  uint8_t inl;      // internal timer: fn runs inside timers_check
  _Atomic uint8_t running;  // internal timer: fn executing
  uint64_t pc[TRACE_FRAMES];  // where it was made (MARMOT_TRACE only)
};

_Atomic int64_t timer_next_when = INT64_MAX;

static rt_spin tlock;
static rt_timer **theap;
static int32_t tlen, tcap;
static rt_timer *fireq_head, *fireq_tail;
static rt_g *timer_g;
static bool timer_g_parked;
static rt_spin timer_g_lock;


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
// First timer: hook the timer code into the scheduler and the GC.
static void timers_use(void) {
  if (__atomic_load_n(&hook_timers_check, __ATOMIC_ACQUIRE)) return;
  __atomic_store_n(&hook_timers_scan, timers_scan_roots, __ATOMIC_RELEASE);
  __atomic_store_n(&hook_sysmon_start, sysmon_start, __ATOMIC_RELEASE);
  __atomic_store_n(&hook_timers_check, timers_check, __ATOMIC_RELEASE);
  // sysmon starts at the next goroutine or thread start (called with the
  // timer lock held: no thread creation here).
}

static void heap_add(rt_timer *t) {
  timers_use();
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

// The frames of the timer whose function timerproc runs (MARMOT_TRACE).
static uint64_t firing_pc[TRACE_FRAMES];
static bool firing;

const uint64_t *timer_firing_frames(void) {
  rt_m *m = getm();
  return firing && m && m->curg == timer_g ? firing_pc : NULL;
}

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
    if (trace_on()) __builtin_memcpy(firing_pc, t->pc, sizeof firing_pc);
    spin_unlock(&tlock);
    firing = trace_on();
    fn(arg);
    firing = false;
  }
}

int64_t timers_check(int64_t now) {
  rt_timer *ready = NULL;
  bool wake = false;
  spin_lock(&tlock);
  while (tlen > 0 && theap[0]->when <= now) {
    rt_timer *t = theap[0];
    heap_remove(t);
    if (t->inl) {
      t->state = T_DONE;
      atomic_store(&t->running, 1);
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
  rt_m *m = trace_on() ? getm() : NULL;
  rt_m *me = getm();
  if (me) me->in_timers = true;
  while (ready) {
    rt_timer *t = ready;
    ready = t->next;
    t->next = NULL;
    if (m) {
      m->wake_next = (trace_why){.cause = WAKE_TIMER};
      __builtin_memcpy(m->wake_next.pc, t->pc, sizeof t->pc);
    }
    t->fn(t->arg);
    atomic_store(&t->running, 0);
  }
  if (wake) {
    if (m) m->wake_next = (trace_why){.cause = WAKE_TIMER};
    rt_ready(timer_g);
  }
  if (me) me->in_timers = false;
  if (m) m->wake_next.cause = 0;
  return next;
}

void timers_due_why(trace_why *w) {
  *w = (trace_why){.cause = WAKE_TIMER};
  spin_lock(&tlock);
  if (tlen) __builtin_memcpy(w->pc, theap[0]->pc, sizeof w->pc);
  spin_unlock(&tlock);
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
  if (trace_on()) {
    const uint64_t *tf = timer_firing_frames();  // re-armed from a timer: its frames
    if (tf)
      __builtin_memcpy(t->pc, tf, sizeof t->pc);
    else
      trace_frames(__builtin_frame_address(0), t->pc);
  }
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

static void sleep_wake(void *gp) { rt_ready((rt_g *)gp); }

rt_timer *itimer_new(void) {
  rt_timer *t = persistent_alloc(sizeof(rt_timer), 8);
  t->inl = 1;
  t->idx = -1;
  return t;
}

void itimer_start(rt_timer *t, int64_t when, void (*fn)(void *), void *arg) {
  t->when = when;
  t->fn = fn;
  t->arg = arg;
  if (trace_on()) trace_frames(__builtin_frame_address(0), t->pc);
  timer_add(t);
}

// Returns once the timer can no longer fire: if its function is running on
// another thread, waits for it to finish.
bool itimer_stop(rt_timer *t) {
  bool r = false;
  spin_lock(&tlock);
  if (t->state == T_PENDING) {
    heap_remove(t);
    update_next();
    t->state = T_STOPPED;
    r = true;
  }
  spin_unlock(&tlock);
  while (atomic_load(&t->running)) os_yield();
  return r;
}

RT_API void rt_sleep(int64_t ns) {
  if (ns <= 0) return;
  rt_m *m = getm();
  rt_g *g = m->curg;
  if (!g->sleeptimer) {
    g->sleeptimer = itimer_new();
    g->sleeptimer->fn = sleep_wake;
    g->sleeptimer->arg = g;
  }
  g->sleeptimer->when = os_nanotime() + ns;
  if (trace_on()) trace_frames(__builtin_frame_address(0), g->sleeptimer->pc);
  rt_park(sleep_park, g);
}

void timers_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi)) {
  if (tlen) scan((uintptr_t)theap, (uintptr_t)(theap + tlen));
  if (fireq_head) scan((uintptr_t)&fireq_head, (uintptr_t)(&fireq_head + 1));
}

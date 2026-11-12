// sched.c: goroutines and the M:N scheduler.
//
// Model (after the gc runtime): G is a goroutine, M an OS thread, P a
// processor slot (GOMAXPROCS of them). An M needs a P to run goroutines.
// Each P has a local run queue (single producer, stealable) and a runnext
// slot; there is a global run queue under sched.lock. Each M runs its
// scheduler loop on g0, its thread stack; goroutines switch to g0 to park,
// yield or exit, and g0 switches to the next goroutine.
//
// Blocking system calls: rt_entersyscall marks the P as P_SYSCALL without
// giving it up. If the call returns quickly the M takes it back with one CAS.
// sysmon, a background thread, retakes Ps that stay in a syscall for more
// than 20us and hands them to other Ms.
//
// Stop the world: the stopping M sets sched.gcwaiting and waits until every
// P is stopped. Ms ack at safepoints (scheduler, allocation slow path, park,
// gosched, syscall entry) or, for tight loops, from a SIGURG handler. An M
// keeps its P across the stop. Before acking, an M records the stack pointer
// below which nothing is live, after spilling callee-saved registers, so the
// GC can scan the stack conservatively.

#include "internal.h"

#include <pthread.h>


schedt sched;
rt_g *rt_main_g;

static rt_m m0;
// Goroutine stacks are address space reserved with mmap and committed page
// by page as they are touched, so a large reservation costs no memory: 64
// MiB lets deep recursion (std's depth-limit tests recurse 10^4-10^5
// levels) run in any goroutine, as gc's growable stacks do. 10^6 goroutines
// reserve 64 TiB, inside the 47-bit address space; if mmap still fails a
// stack falls back to 1 MiB (stack_alloc).
static uintptr_t g_stacksize = (uintptr_t)64 << 20;  // default goroutine stack reservation
static uintptr_t main_stacksize = 1ull << 30;  // main goroutine
static bool sysmon_started;
static rt_spin sysmon_lock;
static fixalloc g_alloc, m_alloc, p_alloc;
static rt_mutex worldsema;

static void schedule_loop(rt_m *m) NORETURN;
void sysmon_start(void);

int64_t (*hook_timers_check)(int64_t now);
void (*hook_timers_scan)(void (*scan)(uintptr_t lo, uintptr_t hi));
int32_t (*hook_netpoll)(int64_t ns);
void (*hook_netpoll_break)(void);
void (*hook_sysmon_start)(void);

INLINE void *hook_get(void *pp) { return __atomic_load_n((void **)pp, __ATOMIC_ACQUIRE); }

static void timers_check_h(int64_t now) {
  int64_t (*f)(int64_t) = hook_get(&hook_timers_check);
  if (f) f(now);
}

static int32_t netpoll_h(int64_t ns) {
  int32_t (*f)(int64_t) = hook_get(&hook_netpoll);
  return f ? f(ns) : 0;
}

static void sysmon_start_h(void) {
  void (*f)(void) = hook_get(&hook_sysmon_start);
  if (f) f();
}

// ---- stacks and goroutine structures ----

static bool stack_alloc(rt_g *g, uintptr_t size) {
  uintptr_t ps = rt_pagesize;
  size = (size + ps - 1) & ~(ps - 1);
  uint8_t *p = os_map(size + ps);
  if (!p && size > ((uintptr_t)1 << 20)) {
    // Out of address space (or a map count limit): a small stack, never
    // cached (gfput only caches g_stacksize stacks).
    size = (uintptr_t)1 << 20;
    p = os_map(size + ps);
  }
  if (!p) return false;
  os_protect_none(p, ps);
  g->map_base = (uintptr_t)p;
  g->map_size = size + ps;
  g->stack_lo = (uintptr_t)p + ps;
  g->stack_hi = (uintptr_t)p + ps + size;
  return true;
}

// Goroutine table: chunks of pointers that never move, with a bitmap of
// goroutines that may be alive, so the GC skips dead ones a word at a time.
#define GCHUNK 4096
typedef struct gchunk {
  rt_g *g[GCHUNK];
  _Atomic uint64_t live[GCHUNK / 64];
} gchunk;
#define MAX_GCHUNKS 16384  // 64M goroutines
static gchunk **gchunks;  // MAX_GCHUNKS entries, allocated on first use
static uint32_t nallg;

static void gtable_add(rt_g *g) {
  spin_lock(&sched.glock);
  uint32_t i = nallg;
  if (i / GCHUNK >= MAX_GCHUNKS) rt_throw("too many goroutines");
  if (!gchunks) gchunks = os_map(MAX_GCHUNKS * sizeof(gchunk *));
  if (i % GCHUNK == 0) gchunks[i / GCHUNK] = persistent_alloc(sizeof(gchunk), 64);
  gchunks[i / GCHUNK]->g[i % GCHUNK] = g;
  g->gidx = i;
  __atomic_store_n(&nallg, i + 1, __ATOMIC_RELEASE);
  spin_unlock(&sched.glock);
}

// Goroutine ids come in blocks of 16 per P, and the count of live user
// goroutines is kept per P (exits may happen on another P than the start,
// so single P counts can go negative; the sum is exact). No shared counter
// is written on spawn or exit.
static int64_t new_goid(rt_p *p) {
  if (!p) return atomic_fetch_add(&sched.goidgen, 1) + 1;
  if (p->goidnext == p->goidend) {
    p->goidnext = atomic_fetch_add(&sched.goidgen, 16) + 1;
    p->goidend = p->goidnext + 16;
  }
  return p->goidnext++;
}

static void ngo_add(rt_p *p, int64_t d) {
  if (p)
    atomic_store_explicit(&p->ngo, atomic_load_explicit(&p->ngo, memory_order_relaxed) + d,
                          memory_order_relaxed);
  else
    atomic_fetch_add(&sched.ngoroutines, d);
}

static int64_t count_goroutines(void) {
  int64_t n = atomic_load(&sched.ngoroutines);
  int32_t na = atomic_load(&sched.nallp);
  for (int32_t i = 0; i < na; i++) n += atomic_load_explicit(&sched.allp[i]->ngo, memory_order_relaxed);
  return n;
}

static void glive_set(rt_g *g, bool live) {
  gchunk *c = gchunks[g->gidx / GCHUNK];
  uint64_t bit = 1ull << (g->gidx % 64);
  if (live)
    atomic_fetch_or(&c->live[(g->gidx % GCHUNK) / 64], bit);
  else
    atomic_fetch_and(&c->live[(g->gidx % GCHUNK) / 64], ~bit);
}

// Allocate a goroutine structure. stacksize 0: the stack is mapped when the
// goroutine first runs (execute), so goroutines created faster than they
// run cost only this structure.
static rt_g *galloc(uintptr_t stacksize) {
  spin_lock(&sched.glock);
  rt_g *g = fixalloc_alloc(&g_alloc);
  spin_unlock(&sched.glock);
  if (stacksize && !stack_alloc(g, stacksize))
    rt_throw("out of memory allocating goroutine stack");
  gtable_add(g);
  return g;
}

// Stacks of size g_stacksize are cached apart from goroutine structures: a
// goroutine gives its stack back when it exits and takes one when it first
// runs. Per-P cache of 32, global cache of up to MAX_FREE_STACKS (linked
// through a word near the top of each stack, which is already resident),
// the rest are unmapped.
#define MAX_FREE_STACKS 1024
static rt_spin stack_lock;
static uintptr_t gstack_free;  // global list head: mapping base
static int32_t ngstack_free;

INLINE uintptr_t *stack_link(uintptr_t base) {
  return (uintptr_t *)(base + rt_pagesize + g_stacksize - 16);
}

static void stack_give(rt_p *p, rt_g *g) {
  uintptr_t base = g->map_base;
  g->map_base = g->map_size = g->stack_lo = g->stack_hi = 0;
  if (p->nstackcache < 32) {
    p->stackcache[p->nstackcache++] = base;
    return;
  }
  // Local cache full: move half of it to the global cache, keep this one.
  spin_lock(&stack_lock);
  while (p->nstackcache > 16) {
    uintptr_t b = p->stackcache[--p->nstackcache];
    if (ngstack_free >= MAX_FREE_STACKS) {
      os_unmap((void *)b, g_stacksize + rt_pagesize);
      continue;
    }
    *stack_link(b) = gstack_free;
    gstack_free = b;
    __atomic_store_n(&ngstack_free, ngstack_free + 1, __ATOMIC_RELAXED);
  }
  spin_unlock(&stack_lock);
  p->stackcache[p->nstackcache++] = base;
}

static void stack_take(rt_p *p, rt_g *g) {
  uintptr_t base = 0;
  if (p && p->nstackcache == 0 && __atomic_load_n(&ngstack_free, __ATOMIC_RELAXED) > 0) {
    spin_lock(&stack_lock);
    while (p->nstackcache < 16 && gstack_free) {
      uintptr_t b = gstack_free;
      gstack_free = *stack_link(b);
      __atomic_store_n(&ngstack_free, ngstack_free - 1, __ATOMIC_RELAXED);
      p->stackcache[p->nstackcache++] = b;
    }
    spin_unlock(&stack_lock);
  }
  if (p && p->nstackcache > 0) base = p->stackcache[--p->nstackcache];
  if (base) {
    uintptr_t ps = rt_pagesize;
    g->map_base = base;
    g->map_size = g_stacksize + ps;
    g->stack_lo = base + ps;
    g->stack_hi = base + ps + g_stacksize;
    return;
  }
  if (!stack_alloc(g, g_stacksize)) rt_throw("out of memory allocating goroutine stack");
}

// Free goroutine structures: per P, overflow to global.
static void gfput(rt_p *p, rt_g *g) {
  if (g == rt_main_g) return;  // never reused
  if (g->stack_hi && g->stack_hi - g->stack_lo != g_stacksize) {
    // A fallback stack (stack_alloc): not cached; the next start takes a
    // standard one.
    os_unmap((void *)g->map_base, g->map_size);
    g->stack_hi = g->stack_lo = g->map_base = g->map_size = 0;
  }
  if (g->stack_hi) stack_give(p, g);
  atomic_store_explicit(&g->status, G_IDLE, memory_order_relaxed);
  g->schedlink = p->gfree;
  p->gfree = g;
  p->ngfree++;
  if (p->ngfree >= 64) {
    spin_lock(&sched.lock);
    while (p->ngfree > 32) {
      rt_g *x = p->gfree;
      p->gfree = x->schedlink;
      p->ngfree--;
      x->schedlink = sched.gfree;
      __atomic_store_n(&sched.gfree, x, __ATOMIC_RELAXED);
      sched.ngfree++;
    }
    spin_unlock(&sched.lock);
  }
}

static rt_g *gfget(rt_p *p) {
  // Unlocked peek at the global list (rechecked under the lock).
  if (!p->gfree && __atomic_load_n(&sched.gfree, __ATOMIC_RELAXED)) {
    spin_lock(&sched.lock);
    while (p->ngfree < 32 && sched.gfree) {
      rt_g *x = sched.gfree;
      __atomic_store_n(&sched.gfree, x->schedlink, __ATOMIC_RELAXED);
      sched.ngfree--;
      x->schedlink = p->gfree;
      p->gfree = x;
      p->ngfree++;
    }
    spin_unlock(&sched.lock);
  }
  rt_g *g = p->gfree;
  if (g) {
    p->gfree = g->schedlink;
    p->ngfree--;
  }
  return g;
}

// ---- run queues ----

static bool runqempty(rt_p *p) {
  for (;;) {
    uint32_t h = atomic_load(&p->runqhead);
    uint32_t t = atomic_load(&p->runqtail);
    rt_g *next = atomic_load(&p->runnext);
    if (t == atomic_load(&p->runqtail)) return h == t && next == NULL;
  }
}

// Called with sched.lock held.
static void globrunqput(rt_g *g) {
  g->schedlink = NULL;
  if (sched.runqtail)
    sched.runqtail->schedlink = g;
  else
    sched.runqhead = g;
  sched.runqtail = g;
  sched.runqsize++;
}

static void globrunqputbatch(rt_g *head, rt_g *tail, int32_t n) {
  tail->schedlink = NULL;
  if (sched.runqtail)
    sched.runqtail->schedlink = head;
  else
    sched.runqhead = head;
  sched.runqtail = tail;
  sched.runqsize += n;
}

// Move half of the local queue plus g to the global queue (queue full).
static bool runqputslow(rt_p *p, rt_g *g, uint32_t h, uint32_t t) {
  rt_g *batch[RUNQ_SIZE / 2 + 1];
  uint32_t n = (t - h) / 2;
  if (n != RUNQ_SIZE / 2) rt_throw("runqputslow: queue is not full");
  for (uint32_t i = 0; i < n; i++)
    batch[i] = atomic_load_explicit(&p->runq[(h + i) % RUNQ_SIZE], memory_order_relaxed);
  if (!atomic_compare_exchange_strong_explicit(&p->runqhead, &h, h + n, memory_order_release,
                                               memory_order_relaxed))
    return false;
  batch[n] = g;
  for (uint32_t i = 0; i < n; i++) batch[i]->schedlink = batch[i + 1];
  spin_lock(&sched.lock);
  globrunqputbatch(batch[0], batch[n], (int32_t)n + 1);
  spin_unlock(&sched.lock);
  return true;
}

// Put g on p's local queue. next: put it in runnext (runs next).
static void runqput(rt_p *p, rt_g *g, bool next) {
  if (next) {
    rt_g *old = atomic_exchange(&p->runnext, g);
    if (!old) return;
    g = old;
  }
  for (;;) {
    uint32_t h = atomic_load_explicit(&p->runqhead, memory_order_acquire);
    uint32_t t = atomic_load_explicit(&p->runqtail, memory_order_relaxed);
    if (t - h < RUNQ_SIZE) {
      atomic_store_explicit(&p->runq[t % RUNQ_SIZE], g, memory_order_relaxed);
      atomic_store_explicit(&p->runqtail, t + 1, memory_order_release);
      return;
    }
    if (runqputslow(p, g, h, t)) return;
  }
}

static rt_g *runqget(rt_p *p) {
  rt_g *next = atomic_load(&p->runnext);
  if (next && atomic_compare_exchange_strong(&p->runnext, &next, NULL)) return next;
  for (;;) {
    uint32_t h = atomic_load_explicit(&p->runqhead, memory_order_acquire);
    uint32_t t = atomic_load_explicit(&p->runqtail, memory_order_relaxed);
    if (t == h) return NULL;
    rt_g *g = atomic_load_explicit(&p->runq[h % RUNQ_SIZE], memory_order_relaxed);
    if (atomic_compare_exchange_strong_explicit(&p->runqhead, &h, h + 1, memory_order_release,
                                                memory_order_relaxed))
      return g;
  }
}

// Grab about half of victim's queue into p's queue starting at p's tail.
static uint32_t runqgrab(rt_p *victim, rt_p *p, uint32_t tail, bool stealnext) {
  for (;;) {
    uint32_t h = atomic_load_explicit(&victim->runqhead, memory_order_acquire);
    uint32_t t = atomic_load_explicit(&victim->runqtail, memory_order_acquire);
    uint32_t n = t - h;
    n = n - n / 2;
    if (n == 0) {
      if (stealnext) {
        rt_g *next = atomic_load(&victim->runnext);
        if (next) {
          // As gc: a running P is usually about to switch to its run-next
          // goroutine (a channel handoff readies the partner there just
          // before the sender blocks). Wait 3 us before taking it, so a
          // ping-pong pair stays on one thread; meanwhile this M still
          // counts as spinning, so rt_ready skips its futex wakeups.
          if (atomic_load_explicit(&victim->status, memory_order_relaxed) == P_RUNNING) {
            os_usleep(3);
            if (atomic_load(&victim->runnext) != next) continue;
          }
          if (!atomic_compare_exchange_strong(&victim->runnext, &next, NULL)) continue;
          atomic_store_explicit(&p->runq[tail % RUNQ_SIZE], next, memory_order_relaxed);
          return 1;
        }
      }
      return 0;
    }
    if (n > RUNQ_SIZE / 2) continue;  // inconsistent snapshot
    for (uint32_t i = 0; i < n; i++)
      atomic_store_explicit(&p->runq[(tail + i) % RUNQ_SIZE],
                            atomic_load_explicit(&victim->runq[(h + i) % RUNQ_SIZE], memory_order_relaxed),
                            memory_order_relaxed);
    if (atomic_compare_exchange_strong_explicit(&victim->runqhead, &h, h + n,
                                                memory_order_release, memory_order_relaxed))
      return n;
  }
}

static rt_g *runqsteal(rt_p *p, rt_p *victim, bool stealnext) {
  uint32_t t = atomic_load_explicit(&p->runqtail, memory_order_relaxed);
  uint32_t n = runqgrab(victim, p, t, stealnext);
  if (n == 0) return NULL;
  n--;
  rt_g *g = atomic_load_explicit(&p->runq[(t + n) % RUNQ_SIZE], memory_order_relaxed);
  if (n > 0) atomic_store_explicit(&p->runqtail, t + n, memory_order_release);
  return g;
}

// Called with sched.lock held. Takes up to max goroutines (0: no limit).
static rt_g *globrunqget(rt_p *p, int32_t max) {
  if (sched.runqsize == 0) return NULL;
  int32_t np = atomic_load(&sched.nprocs);
  int32_t n = sched.runqsize / np + 1;
  if (n > sched.runqsize) n = sched.runqsize;
  if (max > 0 && n > max) n = max;
  if (n > RUNQ_SIZE / 2) n = RUNQ_SIZE / 2;
  sched.runqsize -= n;
  rt_g *g = sched.runqhead;
  sched.runqhead = g->schedlink;
  n--;
  while (n-- > 0) {
    rt_g *x = sched.runqhead;
    sched.runqhead = x->schedlink;
    runqput(p, x, false);
  }
  if (!sched.runqhead) sched.runqtail = NULL;
  return g;
}

// ---- P and M lists (sched.lock held) ----

static void pidleput(rt_p *p) {
  atomic_store(&p->status, P_IDLE);
  p->m = NULL;
  p->link = sched.pidle;
  sched.pidle = p;
  atomic_fetch_add(&sched.npidle, 1);
}

static rt_p *pidleget(void) {
  rt_p *p = sched.pidle;
  if (p) {
    sched.pidle = p->link;
    atomic_fetch_sub(&sched.npidle, 1);
  }
  return p;
}

static void mput(rt_m *m) {
  m->idlelink = sched.midle;
  sched.midle = m;
  m->idle = true;
  sched.nmidle++;
}

static void mremove(rt_m *m) {
  rt_m **pp = &sched.midle;
  while (*pp != m) pp = &(*pp)->idlelink;
  *pp = m->idlelink;
  m->idle = false;
  sched.nmidle--;
  if (sched.timer_m == m) sched.timer_m = NULL;
}

static rt_m *mget(void) {
  rt_m *m = sched.midle;
  if (m) mremove(m);
  return m;
}

static void acquirep(rt_m *m, rt_p *p) {
  m->p = p;
  m->mcache = p->mcache;
  p->m = m;
  atomic_store(&p->status, P_RUNNING);
}

static rt_p *releasep(rt_m *m) {
  rt_p *p = m->p;
  m->p = NULL;
  m->mcache = NULL;
  p->m = NULL;
  return p;
}

// ---- deadlock detection ----

// Called with sched.lock held when an M is about to go idle.
static void checkdead(void) {
  int32_t run = sched.mcount - sched.nmidle;
  if (run > 0) return;
  if (run < 0) rt_throw("checkdead: inconsistent M counts");
  if (atomic_load(&timer_next_when) != INT64_MAX) return;
  if (sched.runqsize > 0) return;
  if (signal_waiting()) return;  // a signal may still arrive
  if (atomic_load(&netpoll_waiters) > 0) return;  // I/O may still arrive
  if (sched.main_exited && count_goroutines() == 0)
    rt_throw("no goroutines (main called runtime.Goexit) - deadlock!");
  rt_throw("all goroutines are asleep - deadlock!");
}

// ---- Ms ----

static void mstart(void *arg) {
  rt_m *m = arg;
  setm(m);
#ifdef RT_TSAN
  m->g0.tsan_fiber = __tsan_get_current_fiber();
#endif
  m->thread = (void *)pthread_self();
  signal_init_thread(m);
  rt_p *p = m->nextp;
  m->nextp = NULL;
  acquirep(m, p);
  schedule_loop(m);
}

static void newm(rt_p *p, bool spinning) {
  spin_lock(&sched.lock);
  rt_m *m = fixalloc_alloc(&m_alloc);
  m->id = sched.mcount++;
  m->alllink = sched.allm;
  sched.allm = m;
  m->fastrand = rt_fastrand() | 1;
  m->nextp = p;
  m->spinning = spinning;
  spin_unlock(&sched.lock);
  rt_m *me = getm();
  if (me) me->locks++;
  bool ok = os_thread_create(mstart, m, 256 << 10);
  if (me) me->locks--;
  if (!ok) rt_throw("newm: cannot create thread");
  sysmon_start_h();
}

// Run some M with p (or an idle P if p is NULL). spinning: the caller
// already incremented nmspinning for it.
static void startm(rt_p *p, bool spinning) {
  spin_lock(&sched.lock);
  if (!p) {
    p = pidleget();
    if (!p) {
      spin_unlock(&sched.lock);
      if (spinning) atomic_fetch_sub(&sched.nmspinning, 1);
      return;
    }
  }
  rt_m *m = mget();
  if (m) {
    m->spinning = spinning;
    m->nextp = p;
    spin_unlock(&sched.lock);
    notewakeup(&m->park);
    return;
  }
  spin_unlock(&sched.lock);
  newm(p, spinning);
}

void wakep(void) {
  if (atomic_load(&sched.npidle) == 0) return;
  int32_t zero = 0;
  if (atomic_load(&sched.nmspinning) != 0 ||
      !atomic_compare_exchange_strong(&sched.nmspinning, &zero, 1))
    return;
  startm(NULL, true);
}

// Hand off a P whose M is blocked or gone.
static void handoffp(rt_p *p) {
  if (!runqempty(p) || sched.runqsize > 0) {
    startm(p, false);
    return;
  }
  int32_t zero = 0;
  if (atomic_load(&sched.nmspinning) + atomic_load(&sched.npidle) == 0 &&
      atomic_compare_exchange_strong(&sched.nmspinning, &zero, 1)) {
    startm(p, true);
    return;
  }
  spin_lock(&sched.lock);
  if (atomic_load(&sched.gcwaiting)) {
    atomic_store(&p->status, P_GCSTOP);
    p->m = NULL;
    if (--sched.stopwait == 0) notewakeup(&sched.stopnote);
    spin_unlock(&sched.lock);
    return;
  }
  if (sched.runqsize > 0 ||
      (atomic_load(&timer_next_when) != INT64_MAX && sched.timer_m == NULL)) {
    spin_unlock(&sched.lock);
    startm(p, false);
    return;
  }
  if (p->id >= atomic_load(&sched.nprocs)) {
    atomic_store(&p->status, P_DEAD);
    p->m = NULL;
  } else {
    pidleput(p);
  }
  spin_unlock(&sched.lock);
}

void sched_timer_added(int64_t when) {
  void (*brk)(void) = hook_get(&hook_netpoll_break);
  if (brk && atomic_load(&netpoll_blocked)) brk();
  spin_lock(&sched.lock);
  rt_m *tm = sched.timer_m;
  if (tm && when < sched.timer_until) {
    mremove(tm);
    tm->nextp = NULL;
    spin_unlock(&sched.lock);
    notewakeup(&tm->park);
    return;
  }
  bool none = tm == NULL;
  spin_unlock(&sched.lock);
  if (none) wakep();
}

// ---- stop the world ----

void sched_stop_self(void *sp, uintptr_t hi) {
  rt_m *m = getm();
  rt_p *p = m->p;
  m->stop_sp = sp;
  m->stop_hi = hi;
  spin_lock(&sched.lock);
  if (!atomic_load(&sched.gcwaiting) || !p || atomic_load(&p->status) != P_RUNNING) {
    spin_unlock(&sched.lock);
    return;
  }
  atomic_store(&p->status, P_GCSTOP);
  uint32_t gen = atomic_load(&sched.worldgen);
  if (--sched.stopwait == 0) notewakeup(&sched.stopnote);
  spin_unlock(&sched.lock);
  while (atomic_load(&sched.worldgen) == gen) os_futex_wait(&sched.worldgen, gen, -1);
}

NOINLINE void rt_safepoint(void) {
  if (!atomic_load_explicit(&sched.gcwaiting, memory_order_acquire)) return;
  rt_m *m = getm();
  if (!m || !m->curg || m->locks > 0) return;
  void *regs[24];
  void *sp = rt_spill_regs(regs);
  sched_stop_self(sp, m->curg->stack_hi);
  __asm__ volatile("" ::"r"(regs) : "memory");
}

void stop_the_world(void) {
  rt_lock(&worldsema);
  rt_m *m = getm();
  m->locks++;
  spin_lock(&sched.lock);
  noteclear(&sched.stopnote);
  int32_t n = atomic_load(&sched.nallp);
  int32_t wait = 0;
  for (int32_t i = 0; i < n; i++)
    if (atomic_load(&sched.allp[i]->status) != P_DEAD) wait++;
  sched.stopwait = wait;
  atomic_store(&sched.gcwaiting, 1);
  atomic_store(&m->p->status, P_GCSTOP);
  sched.stopwait--;
  for (int32_t i = 0; i < n; i++) {
    rt_p *p = sched.allp[i];
    uint32_t s = P_SYSCALL;
    if (atomic_compare_exchange_strong(&p->status, &s, P_GCSTOP)) {
      p->m = NULL;
      atomic_fetch_add(&p->syscalltick, 1);
      sched.stopwait--;
    }
  }
  rt_p *p;
  while ((p = pidleget()) != NULL) {
    atomic_store(&p->status, P_GCSTOP);
    sched.stopwait--;
  }
  bool waiting = sched.stopwait > 0;
  spin_unlock(&sched.lock);
  while (waiting) {
    if (notetsleep(&sched.stopnote, 50000)) break;
    spin_lock(&sched.lock);
    for (int32_t i = 0; i < n; i++) {
      rt_p *q = sched.allp[i];
      uint32_t s = P_SYSCALL;
      if (atomic_compare_exchange_strong(&q->status, &s, P_GCSTOP)) {
        q->m = NULL;
        atomic_fetch_add(&q->syscalltick, 1);
        sched.stopwait--;
      } else if (s == P_RUNNING) {
        // q->m is written by an M leaving a syscall without sched.lock; a
        // stale value only delays the signal to the next round.
        rt_m *qm = __atomic_load_n(&q->m, __ATOMIC_RELAXED);
        if (qm && qm != m) signal_preempt(qm);
      }
    }
    waiting = sched.stopwait > 0;
    spin_unlock(&sched.lock);
  }
  if (sched.stopwait != 0) rt_throw("stop_the_world: stopwait != 0");
  m->locks--;
}

void start_the_world(void) {
  rt_m *m = getm();
  m->locks++;
  spin_lock(&sched.lock);
  int32_t n = atomic_load(&sched.nallp);
  int32_t np = atomic_load(&sched.nprocs);
  for (int32_t i = 0; i < n; i++) {
    rt_p *p = sched.allp[i];
    if (atomic_load(&p->status) != P_GCSTOP) continue;
    if (p->m)
      atomic_store(&p->status, P_RUNNING);
    else if (p->id < np)
      pidleput(p);
    else
      atomic_store(&p->status, P_DEAD);
  }
  atomic_store(&sched.gcwaiting, 0);
  atomic_fetch_add(&sched.worldgen, 1);
  os_futex_wake(&sched.worldgen, true);
  bool work = sched.runqsize > 0;
  for (int32_t i = 0; i < n && !work; i++)
    if (atomic_load(&sched.allp[i]->status) != P_DEAD && sched.allp[i] != m->p &&
        !runqempty(sched.allp[i]))
      work = true;
  spin_unlock(&sched.lock);
  m->locks--;
  rt_unlock(&worldsema);
  // Wake a thread only for queued work: a spinning thread holds a P, and on
  // a loaded machine the next stop the world would wait for it to run.
  if (work) wakep();
}

// ---- switching ----

// Switch from the current goroutine to g0 with a request. Returns when the
// goroutine is resumed, possibly on another M.
static void mcall(int32_t req) {
  rt_m *m = getm();
  rt_g *g = m->curg;
  m->locks++;
  m->req = req;
#ifdef RT_TSAN
  __tsan_switch_to_fiber(m->g0.tsan_fiber, 0);
#endif
  rt_ctx_switch(&g->sp, m->g0.sp);
  getm()->locks--;
}

static void goexit_final(rt_g *g);

static void gstart(void *arg) {
  rt_g *g = arg;
  getm()->locks--;  // taken by execute
  void (*fn)(void *) = g->fn;
  void *a = g->arg;
  g->arg = NULL;
  fn(a);
  if (g == rt_main_g) rt_exit(0);
  mcall(REQ_EXIT);
  __builtin_unreachable();
}

static void execute(rt_m *m, rt_g *g) {
  if (UNLIKELY(!g->sp)) {
    // First run: take a stack now.
    if (!g->stack_hi) stack_take(m->p, g);
    g->sp = rt_ctx_init((void *)g->stack_hi, gstart, g);
  }
  m->curg = g;
  g->m = m;
  atomic_store_explicit(&g->status, G_RUNNING, memory_order_relaxed);
  atomic_fetch_add_explicit(&m->p->schedtick, 1, memory_order_relaxed);
  m->locks++;
#ifdef RT_TSAN
  if (!g->tsan_fiber) g->tsan_fiber = __tsan_create_fiber(0);
  __tsan_switch_to_fiber(g->tsan_fiber, 0);
#endif
  rt_ctx_switch(&m->g0.sp, g->sp);
  m->locks--;
}

static void goexit_final(rt_g *g) {
  if (!g->system) ngo_add(getm()->p, -1);
  if (g == rt_main_g) {
    spin_lock(&sched.lock);
    sched.main_exited = true;
    spin_unlock(&sched.lock);
  }
  g->fn = NULL;
  g->arg = NULL;
  g->param = NULL;
  atomic_store(&g->status, G_DEAD);
  // slot survives for the next goroutine that reuses this structure (the Go
  // runtime recycles its per-goroutine data); the GC keeps scanning it, so the
  // table bit stays set while the slot is in use.
  if (!g->slot) glive_set(g, false);
}

INLINE void cpu_relax_sched(void) {
#if RT_ARCH_ARM64
  __asm__ volatile("yield");
#else
  __asm__ volatile("pause");
#endif
}

_Atomic int64_t pfor_last;  // monotonic time of the last rt_parallel_for

static void resetspinning(rt_m *m) {
  m->spinning = false;
  if (atomic_fetch_sub(&sched.nmspinning, 1) == 1) wakep();
}

static rt_g *stealwork(rt_m *m) {
  int32_t n = atomic_load(&sched.nallp);
  for (int iter = 0; iter < 4; iter++) {
    uint32_t start = (uint32_t)rt_fastrand();
    for (int32_t i = 0; i < n; i++) {
      if (atomic_load(&sched.gcwaiting)) return NULL;
      rt_p *v = sched.allp[(start + (uint32_t)i) % (uint32_t)n];
      if (v == m->p || atomic_load(&v->status) == P_DEAD) continue;
      rt_g *g = runqsteal(m->p, v, iter == 3);
      if (g) return g;
    }
  }
  return NULL;
}

// Find a runnable goroutine. Blocks. Returns with m->p held.
static rt_g *findrunnable(rt_m *m) {
  rt_g *g;
  if (!m->p) goto stop;
  if (m->handoff_g && !atomic_load(&sched.gcwaiting)) {
    g = m->handoff_g;
    m->handoff_g = NULL;
    goto found;
  }
top:
  if (m->handoff_g) {
    // A stop the world is pending: queue the handoff target normally.
    g = m->handoff_g;
    m->handoff_g = NULL;
    runqput(m->p, g, true);
  }
  if (atomic_load(&sched.gcwaiting)) {
    sched_stop_self(NULL, 0);
    goto top;
  }
  {
    rt_p *p = m->p;
    if (p->id >= atomic_load(&sched.nprocs)) {
      // GOMAXPROCS shrank: drain this P and retire it.
      spin_lock(&sched.lock);
      while ((g = runqget(p)) != NULL) globrunqput(g);
      releasep(m);
      atomic_store(&p->status, P_DEAD);
      // A stop the world that began after our gcwaiting check counted
      // this P as running.
      if (atomic_load(&sched.gcwaiting) && --sched.stopwait == 0) notewakeup(&sched.stopnote);
      spin_unlock(&sched.lock);
      if (m->spinning) {
        m->spinning = false;
        atomic_fetch_sub(&sched.nmspinning, 1);
      }
      goto stop;
    }
    int64_t now = 0;
    if (atomic_load_explicit(&timer_next_when, memory_order_relaxed) != INT64_MAX) {
      now = os_nanotime();
      if (now >= atomic_load(&timer_next_when)) timers_check_h(now);
    }
    if (p->schedtick % 61 == 0 && sched.runqsize > 0) {
      spin_lock(&sched.lock);
      g = globrunqget(p, 1);
      spin_unlock(&sched.lock);
      if (g) goto found;
    }
    if ((g = runqget(p)) != NULL) goto found;
    if (sched.runqsize > 0) {
      spin_lock(&sched.lock);
      g = globrunqget(p, 0);
      spin_unlock(&sched.lock);
      if (g) goto found;
    }
    if (atomic_load(&netpoll_waiters) > 0 && !atomic_load(&netpoll_blocked) && netpoll_h(0) > 0 &&
        (g = runqget(p)) != NULL)
      goto found;
    int32_t np = atomic_load(&sched.nprocs);
    if (np > 1 && (m->spinning ||
                   2 * atomic_load(&sched.nmspinning) < np - atomic_load(&sched.npidle))) {
      if (!m->spinning) {
        m->spinning = true;
        atomic_fetch_add(&sched.nmspinning, 1);
      }
      if ((g = stealwork(m)) != NULL) goto found;
    }
    // Fork-join loops (rt_parallel_for) run in quick succession: right
    // after one, keep looking for work for a short while instead of parking,
    // so the next loop's helpers start without a thread wakeup.
    if (np > 1 && os_nanotime() - atomic_load_explicit(&pfor_last, memory_order_relaxed) < 500000) {
      if (!m->spinning) {
        m->spinning = true;
        atomic_fetch_add(&sched.nmspinning, 1);
      }
      int64_t until = os_nanotime() + 100000;
      while (os_nanotime() < until) {
        if (atomic_load(&sched.gcwaiting)) goto top;
        if ((g = runqget(p)) != NULL) goto found;
        if (sched.runqsize > 0) {
          spin_lock(&sched.lock);
          g = globrunqget(p, 0);
          spin_unlock(&sched.lock);
          if (g) goto found;
        }
        if ((g = stealwork(m)) != NULL) goto found;
        for (int k = 0; k < 32; k++) cpu_relax_sched();
      }
    }
    // Nothing to do: release the P.
    spin_lock(&sched.lock);
    if (atomic_load(&sched.gcwaiting)) {
      spin_unlock(&sched.lock);
      goto top;
    }
    if (sched.runqsize > 0) {
      g = globrunqget(p, 0);
      spin_unlock(&sched.lock);
      goto found;
    }
    releasep(m);
    pidleput(p);
    spin_unlock(&sched.lock);
    bool was_spinning = m->spinning;
    if (m->spinning) {
      m->spinning = false;
      atomic_fetch_sub(&sched.nmspinning, 1);
    }
    // Recheck all queues: a goroutine may have been queued after the
    // check above while nobody saw our P as idle, or while we were still
    // counted as spinning (rt_ready then skips wakep). The global queue
    // first: Ms without a P (syscall exit, sysmon, timers) queue there.
    if (atomic_load(&sched.runqsize) > 0) {
      spin_lock(&sched.lock);
      rt_p *q = sched.runqsize > 0 && !atomic_load(&sched.gcwaiting) ? pidleget() : NULL;
      spin_unlock(&sched.lock);
      if (q) {
        acquirep(m, q);
        goto top;
      }
    }
    int32_t n = atomic_load(&sched.nallp);
    for (int32_t i = 0; i < n; i++) {
      rt_p *v = sched.allp[i];
      if (atomic_load(&v->status) != P_DEAD && !runqempty(v)) {
        spin_lock(&sched.lock);
        rt_p *q = pidleget();
        spin_unlock(&sched.lock);
        if (q) {
          acquirep(m, q);
          if (was_spinning) {
            m->spinning = true;
            atomic_fetch_add(&sched.nmspinning, 1);
          }
          goto top;
        }
        break;
      }
    }
  }
stop:
  for (;;) {
    // Goroutines wait for I/O and nobody polls: block in the poller, with a
    // timeout for the next timer.
    int32_t zero = 0;
    if (atomic_load(&netpoll_waiters) > 0 &&
        atomic_compare_exchange_strong(&netpoll_blocked, &zero, 1)) {
      int64_t next = atomic_load(&timer_next_when);
      int64_t now = os_nanotime();
      int64_t timeout = next == INT64_MAX ? -1 : (next > now ? next - now : 0);
      if (atomic_load(&netpoll_waiters) > 0 && !atomic_load(&sched.gcwaiting)) netpoll_h(timeout);
      atomic_store(&netpoll_blocked, 0);
      if (atomic_load(&timer_next_when) <= os_nanotime()) timers_check_h(os_nanotime());
      spin_lock(&sched.lock);
      rt_p *p = NULL;
      if (!atomic_load(&sched.gcwaiting)) p = pidleget();
      spin_unlock(&sched.lock);
      if (p) {
        acquirep(m, p);
        goto top;
      }
      continue;
    }
    spin_lock(&sched.lock);
    int64_t timeout = -1;
    int64_t next = atomic_load(&timer_next_when);
    if (next != INT64_MAX && sched.timer_m == NULL) {
      int64_t now = os_nanotime();
      timeout = next > now ? next - now : 0;
      sched.timer_m = m;
      sched.timer_until = next;
    }
    m->nextp = NULL;
    noteclear(&m->park);
    mput(m);
    checkdead();
    spin_unlock(&sched.lock);
    if (!notetsleep(&m->park, timeout)) {
      spin_lock(&sched.lock);
      bool pending = !m->idle;  // a waker already took us off the list
      if (m->idle) mremove(m);
      spin_unlock(&sched.lock);
      if (pending) notesleep(&m->park);
    }
    if (m->nextp) {
      rt_p *p = m->nextp;
      m->nextp = NULL;
      acquirep(m, p);
      goto top;
    }
    // Woken for timers, without a P.
    if (atomic_load(&timer_next_when) <= os_nanotime()) timers_check_h(os_nanotime());
    spin_lock(&sched.lock);
    rt_p *p = NULL;
    if (!atomic_load(&sched.gcwaiting)) p = pidleget();
    spin_unlock(&sched.lock);
    if (p) {
      acquirep(m, p);
      goto top;
    }
  }
found:
  if (m->spinning) resetspinning(m);
  return g;
}

static void schedule_loop(rt_m *m) {
  for (;;) {
    rt_g *g = findrunnable(m);
    execute(m, g);
    // Back on g0: handle the goroutine's request.
    m->curg = NULL;
    g->m = NULL;
    switch (m->req) {
      case REQ_GOSCHED:
        atomic_store(&g->status, G_RUNNABLE);
        spin_lock(&sched.lock);
        globrunqput(g);
        spin_unlock(&sched.lock);
        break;
      case REQ_PARK: {
        void (*f)(void *) = m->unlockf;
        void *arg = m->unlockarg;
        m->unlockf = NULL;
        m->unlockarg = NULL;
        atomic_store_explicit(&g->status, G_WAITING, memory_order_release);
        if (f) f(arg);
        break;
      }
      case REQ_EXIT:
        goexit_final(g);
        if (m->p) gfput(m->p, g);
        break;
      case REQ_YIELD:
        atomic_store(&g->status, G_RUNNABLE);
        if (m->p)
          runqput(m->p, g, false);
        else {
          spin_lock(&sched.lock);
          globrunqput(g);
          spin_unlock(&sched.lock);
        }
        break;
      case REQ_HANDOFF: {
        void (*f)(void *) = m->unlockf;
        void *arg = m->unlockarg;
        m->unlockf = NULL;
        m->unlockarg = NULL;
        atomic_store_explicit(&g->status, G_WAITING, memory_order_release);
        if (f) f(arg);
        // m->handoff_g runs next (findrunnable takes it first).
        break;
      }
      case REQ_EXITSYSCALL: {
        // Back from a syscall without a P. Take an idle P or queue g in the
        // same critical section: a P that sysmon hands off (handoffp) after
        // an earlier failed pidleget would otherwise go idle while g sits in
        // the global queue and every M sleeps (lost wakeup).
        atomic_store(&g->status, G_RUNNABLE);
        spin_lock(&sched.lock);
        rt_p *p = atomic_load(&sched.gcwaiting) ? NULL : pidleget();
        if (!p) globrunqput(g);
        spin_unlock(&sched.lock);
        if (p) {
          acquirep(m, p);
          runqput(p, g, true);
        }
        break;
      }
      default:
        rt_throw("schedule: bad request");
    }
    m->req = REQ_NONE;
  }
}

// ---- public goroutine API ----

static rt_g *newproc(void (*fn)(void *), void *arg, bool system) {
  rt_m *m = getm();
  m->locks++;
  rt_p *p = m->p;
  rt_g *g = p ? gfget(p) : NULL;
  if (!g) g = galloc(0);
  g->fn = fn;
  g->arg = arg;
  g->param = NULL;  // slot is kept from the previous user of this structure
  g->system = system;
  g->preempt = false;
  g->id = new_goid(p);
  g->sp = NULL;  // context built by execute
  if (!g->slot) glive_set(g, true);  // a kept slot kept the bit set
  if (!system) ngo_add(p, 1);
  atomic_store_explicit(&g->status, G_RUNNABLE, memory_order_release);
  if (p && atomic_load(&p->status) == P_RUNNING) {
    runqput(p, g, true);
  } else {
    spin_lock(&sched.lock);
    globrunqput(g);
    spin_unlock(&sched.lock);
  }
  m->locks--;
  sysmon_start_h();
  wakep();
  return g;
}

// A second goroutine: the program needs sysmon (syscall retake) and gets
// the parallel collector. Also called by rt_parallel_for, whose helpers are
// system goroutines.
void sched_multi(void) {
  if (UNLIKELY(!hook_get(&hook_sysmon_start))) {
    __atomic_store_n(&hook_sysmon_start, sysmon_start, __ATOMIC_RELEASE);
    gc_parallel_enable();
  }
}

RT_API void rt_go(void (*fn)(void *), void *arg) {
  sched_multi();
  newproc(fn, arg, false);
}

rt_g *new_system_g(void (*fn)(void *), void *arg) { return newproc(fn, arg, true); }

RT_API rt_g *rt_current(void) { return getm()->curg; }

RT_API int64_t rt_goid(void) { return getm()->curg->id; }

RT_API void rt_gosched(void) {
  rt_safepoint();
  mcall(REQ_GOSCHED);
}

// Active spinning for sync.Mutex, as gc's sync_runtime_canSpin: only a few
// rounds, only with several CPUs, only if another P is running (so the
// holder can make progress) and this P has nothing else to run.
static int32_t ncpu_cached;

RT_API int32_t rt_can_spin(int32_t iter) {
  if (iter >= 4) return 0;
  if (!ncpu_cached) ncpu_cached = os_ncpu();
  if (ncpu_cached <= 1) return 0;
  if (atomic_load(&sched.nprocs) <= atomic_load(&sched.npidle) + atomic_load(&sched.nmspinning) + 1)
    return 0;
  rt_m *m = getm();
  if (!m || !m->p || !runqempty(m->p)) return 0;
  return 1;
}

RT_API void rt_procyield(int32_t n) {
  for (int32_t i = 0; i < n; i++) cpu_relax_sched();
}

RT_API void rt_yield(void) {
  rt_safepoint();
  mcall(REQ_YIELD);
}

RT_API void rt_park(void (*unlock)(void *), void *lock) {
  rt_m *m = getm();
  m->unlockf = unlock;
  m->unlockarg = lock;
  mcall(REQ_PARK);
  rt_safepoint();
}

RT_API void rt_handoff(rt_g *target, void (*unlock)(void *), void *lock) {
  uint32_t s = G_WAITING;
  if (!atomic_compare_exchange_strong(&target->status, &s, G_RUNNABLE))
    rt_throw("rt_handoff: goroutine is not waiting");
  rt_m *m = getm();
  m->handoff_g = target;
  m->unlockf = unlock;
  m->unlockarg = lock;
  mcall(REQ_HANDOFF);
  rt_safepoint();
}

RT_API void rt_ready(rt_g *g) {
  uint32_t s = G_WAITING;
  if (!atomic_compare_exchange_strong(&g->status, &s, G_RUNNABLE))
    rt_throw("rt_ready: goroutine is not waiting");
  rt_m *m = getm();
  if (m) m->locks++;
  rt_p *p = m ? m->p : NULL;
  if (p && atomic_load(&p->status) == P_RUNNING && p->m == m) {
    runqput(p, g, true);
  } else {
    spin_lock(&sched.lock);
    globrunqput(g);
    spin_unlock(&sched.lock);
  }
  if (m) m->locks--;
  wakep();
}

RT_API void rt_goexit(void) {
  mcall(REQ_EXIT);
  __builtin_unreachable();
}

RT_API void **rt_gslot(void) { return &getm()->curg->slot; }

RT_API void **rt_gparam(rt_g *g) { return &g->param; }

RT_API intptr_t rt_numgoroutine(void) { return (intptr_t)count_goroutines(); }

RT_API int32_t rt_numcpu(void) { return os_ncpu(); }

static rt_p *pnew(int32_t id) {
  rt_p *p = fixalloc_alloc(&p_alloc);
  p->id = id;
  p->mcache = mcache_new();
  return p;
}

RT_API int32_t rt_gomaxprocs(int32_t n) {
  int32_t old = atomic_load(&sched.nprocs);
  if (n < 1 || n == old) return old;
  if (n > 256) n = 256;
  stop_the_world();
  spin_lock(&sched.lock);
  int32_t na = atomic_load(&sched.nallp);
  for (int32_t i = na; i < n; i++) {
    rt_p *p = pnew(i);
    atomic_store(&p->status, P_GCSTOP);
    sched.allp[i] = p;
  }
  if (n > na) atomic_store(&sched.nallp, n);
  for (int32_t i = 0; i < na && i < n; i++) {
    rt_p *p = sched.allp[i];
    if (atomic_load(&p->status) == P_DEAD) atomic_store(&p->status, P_GCSTOP);
  }
  atomic_store(&sched.nprocs, n);
  spin_unlock(&sched.lock);
  // The current M may now hold a retired P; it gives it up at its next
  // scheduling point, as do other Ms.
  start_the_world();
  if (n > 1) sysmon_start();
  return old;
}

// ---- system calls ----

RT_API void rt_entersyscall(void) {
  rt_m *m = getm();
  if (!m || !m->curg || !m->p) return;
  rt_g *g = m->curg;
  g->syscallsp = rt_spill_regs(g->syscallregs);
  atomic_store(&g->status, G_SYSCALL);
  rt_p *p = m->p;
  atomic_fetch_add(&p->syscalltick, 1);
  // A P in a syscall belongs to no M (as in gc): sysmon or stop the world
  // may take it, another M may run it and enter a syscall itself, and
  // whoever leaves its syscall first and wins the CAS in rt_exitsyscall
  // owns it. With p->m left set, an M returning to a P that another M had
  // since taken into its own syscall won the CAS while p->m still named the
  // other M: two Ms ran on one P, and stop the world signalled the wrong M
  // and waited forever (seen as a hang with one M blocked on gc_mu).
  __atomic_store_n(&p->m, NULL, __ATOMIC_RELAXED);
  atomic_store(&p->status, P_SYSCALL);
  if (atomic_load(&sched.gcwaiting)) {
    spin_lock(&sched.lock);
    uint32_t s = P_SYSCALL;
    if (atomic_load(&sched.gcwaiting) &&
        atomic_compare_exchange_strong(&p->status, &s, P_GCSTOP)) {
      p->m = NULL;
      if (--sched.stopwait == 0) notewakeup(&sched.stopnote);
    }
    spin_unlock(&sched.lock);
  }
}

RT_API void rt_exitsyscall(void) {
  rt_m *m = getm();
  if (!m || !m->curg) return;
  rt_g *g = m->curg;
  if (atomic_load_explicit(&g->status, memory_order_relaxed) != G_SYSCALL) return;
  rt_p *p = m->p;
  uint32_t s = P_SYSCALL;
  if (p && atomic_compare_exchange_strong(&p->status, &s, P_RUNNING)) {
    __atomic_store_n(&p->m, m, __ATOMIC_RELAXED);  // ours again (m->p, m->mcache still point at it)
    atomic_store(&g->status, G_RUNNING);
    return;
  }
  // The P was retaken. Try an idle one, else queue this goroutine.
  m->p = NULL;
  m->mcache = NULL;
  spin_lock(&sched.lock);
  p = atomic_load(&sched.gcwaiting) ? NULL : pidleget();
  spin_unlock(&sched.lock);
  if (p) {
    acquirep(m, p);
    atomic_store(&g->status, G_RUNNING);
    return;
  }
  mcall(REQ_EXITSYSCALL);
}

// ---- sysmon ----

static int retake(int64_t now) {
  int n = 0;
  int32_t na = atomic_load(&sched.nallp);
  for (int32_t i = 0; i < na; i++) {
    rt_p *p = sched.allp[i];
    if (atomic_load(&p->status) != P_SYSCALL) continue;
    uint32_t t = atomic_load(&p->syscalltick);
    if (p->sysmon_tick != t) {
      p->sysmon_tick = t;
      p->sysmon_when = now;
      continue;
    }
    if (runqempty(p) && atomic_load(&sched.nmspinning) + atomic_load(&sched.npidle) > 0 &&
        now - p->sysmon_when < 10000000)
      continue;
    if (now - p->sysmon_when < 20000) continue;
    uint32_t s = P_SYSCALL;
    if (atomic_compare_exchange_strong(&p->status, &s, P_IDLE)) {
      p->m = NULL;
      atomic_fetch_add(&p->syscalltick, 1);
      n++;
      handoffp(p);
    }
  }
  return n;
}

static rt_m sysmon_m;

// Signal handlers bump this to wake sysmon early.
_Atomic uint32_t sysmon_wake;

void sysmon_kick(void) {
  atomic_fetch_add(&sysmon_wake, 1);
  os_futex_wake(&sysmon_wake, false);
}

static void sysmon(void *arg) {
  setm(&sysmon_m);
  uint32_t delay = 20;
  int idle = 0;
  for (;;) {
    uint32_t w = atomic_load(&sysmon_wake);
    os_futex_wait(&sysmon_wake, w, (int64_t)delay * 1000);
    if (signal_deliver()) idle = 0;
    signal_check_stuck();
    if (idle > 50) {
      delay *= 2;
      if (delay > 10000) delay = 10000;
    } else {
      delay = 20;
    }
    if (atomic_load(&sched.gcwaiting)) continue;
    int64_t now = os_nanotime();
    int got = retake(now);
    if (atomic_load(&netpoll_waiters) > 0 && !atomic_load(&netpoll_blocked) &&
        now - atomic_load(&netpoll_last) > 10000000 && netpoll_h(0) > 0)
      got++;
    // Timers overdue while no M is waiting for them.
    int64_t next = atomic_load(&timer_next_when);
    if (next != INT64_MAX && next <= now && sched.timer_m == NULL) {
      wakep();
      got++;
    }
    idle = got ? 0 : idle + 1;
  }
}

void sysmon_start(void) {
  if (__atomic_load_n(&sysmon_started, __ATOMIC_ACQUIRE)) return;
  spin_lock(&sysmon_lock);
  if (!sysmon_started) {
    if (!os_thread_create(sysmon, NULL, 64 << 10)) rt_throw("cannot create sysmon thread");
    __atomic_store_n(&sysmon_started, true, __ATOMIC_RELEASE);
  }
  spin_unlock(&sysmon_lock);
}

// ---- MARMOT_DEADLOCK_DEBUG ----

static const char *pstatus_name(uint32_t s) {
  static const char *names[] = {"idle", "running", "syscall", "gcstop", "dead"};
  return s < 5 ? names[s] : "?";
}

static const char *gstatus_name(uint32_t s) {
  switch (s) {
    case G_IDLE: return "idle";
    case G_RUNNABLE: return "runnable";
    case G_RUNNING: return "running";
    case G_SYSCALL: return "syscall";
    case G_WAITING: return "waiting";
    case G_DEAD: return "dead";
  }
  return "?";
}

static void kv(const char *k, int64_t v) {
  rt_print(" ");
  rt_print(k);
  rt_print("=");
  rt_printi(v);
}

// Racy snapshot from a thread outside the scheduler: no locks, so it works
// while the scheduler is stuck holding one.
void sched_dump(void) {
  rt_print("== marmot scheduler state\nsched:");
  kv("gcwaiting", atomic_load(&sched.gcwaiting));
  kv("stopwait", sched.stopwait);
  kv("stopnote", atomic_load(&sched.stopnote.key));
  kv("worldgen", atomic_load(&sched.worldgen));
  kv("worldsema", __atomic_load_n((uint32_t *)&worldsema.state, __ATOMIC_RELAXED));
  kv("gc_mu", gc_mu_state());
  kv("lock", atomic_load(&sched.lock.v));
  kv("runqsize", atomic_load(&sched.runqsize));
  kv("npidle", atomic_load(&sched.npidle));
  kv("nmspinning", atomic_load(&sched.nmspinning));
  kv("nmidle", sched.nmidle);
  kv("mcount", sched.mcount);
  kv("nprocs", atomic_load(&sched.nprocs));
  kv("ngoroutines", atomic_load(&sched.ngoroutines));
  kv("timer_m", sched.timer_m ? sched.timer_m->id : -1);
  kv("timer_next_ms", atomic_load(&timer_next_when) == INT64_MAX
                          ? -1
                          : (atomic_load(&timer_next_when) - os_nanotime()) / 1000000);
  kv("netpoll_waiters", atomic_load(&netpoll_waiters));
  kv("netpoll_blocked", atomic_load(&netpoll_blocked));
  rt_print("\n");
  int32_t np = atomic_load(&sched.nallp);
  for (int32_t i = 0; i < np; i++) {
    rt_p *p = sched.allp[i];
    rt_print("P");
    rt_printi(i);
    rt_print(": ");
    rt_print(pstatus_name(atomic_load(&p->status)));
    rt_m *pm = p->m;
    kv("m", pm ? pm->id : -1);
    kv("runq", (int64_t)(atomic_load(&p->runqtail) - atomic_load(&p->runqhead)));
    rt_g *rn = atomic_load(&p->runnext);
    kv("runnext", rn ? rn->id : -1);
    kv("schedtick", atomic_load(&p->schedtick));
    kv("syscalltick", atomic_load(&p->syscalltick));
    rt_print("\n");
  }
  for (rt_m *m = sched.allm; m; m = m->alllink) {
    rt_print("M");
    rt_printi(m->id);
    rt_print(":");
    kv("p", m->p ? m->p->id : -1);
    kv("curg", m->curg ? m->curg->id : -1);
    kv("locks", m->locks);
    kv("spinning", m->spinning);
    kv("idle", m->idle);
    kv("nextp", m->nextp ? m->nextp->id : -1);
    kv("req", m->req);
    kv("park", atomic_load(&m->park.key));
    kv("sigurg", atomic_load(&m->nsigurg));
    kv("sigstop", atomic_load(&m->nsigstop));
    kv("sigurg_why", atomic_load(&m->sigurg_why));
    rt_print("\n");
  }
  uint32_t n = __atomic_load_n(&nallg, __ATOMIC_ACQUIRE), shown = 0;
  for (uint32_t i = 0; i < n && shown < 200; i++) {
    rt_g *g = gchunks[i / GCHUNK]->g[i % GCHUNK];
    uint32_t st = atomic_load(&g->status);
    if ((st == G_DEAD || st == G_IDLE) && !g->stack_hi) continue;
    shown++;
    rt_print("G");
    rt_printi(g->id);
    rt_print(": ");
    rt_print(gstatus_name(st));
    rt_m *gm = g->m;
    kv("m", gm ? gm->id : -1);
    if (g->system) rt_print(" system");
    if (g == rt_main_g) rt_print(" main");
    rt_print(" stack=");
    rt_printx(g->stack_lo);
    rt_print("-");
    rt_printx(g->stack_hi);
    rt_print(" sp=");
    rt_printx((uintptr_t)g->sp);
    rt_print("\n");
  }
  rt_print("== end\n");
}

// Sum of scheduling events: unchanged while nothing is scheduled.
uint64_t sched_progress(void) {
  uint64_t t = atomic_load(&sched.worldgen);
  int32_t np = atomic_load(&sched.nallp);
  for (int32_t i = 0; i < np; i++)
    t += atomic_load(&sched.allp[i]->schedtick) + atomic_load(&sched.allp[i]->syscalltick);
  return t;
}

// ---- GC support ----

static void scan_g(rt_g *g, void (*scan)(uintptr_t lo, uintptr_t hi)) {
  uint32_t s = atomic_load(&g->status);
  if (s == G_IDLE || s == G_DEAD) {
    scan((uintptr_t)&g->slot, (uintptr_t)(&g->slot + 1));  // kept for reuse
    return;
  }
  scan((uintptr_t)&g->arg, (uintptr_t)(&g->arg + 1));
  scan((uintptr_t)&g->slot, (uintptr_t)(&g->slot + 1));
  scan((uintptr_t)&g->param, (uintptr_t)(&g->param + 1));
  if (s == G_RUNNABLE && !g->sp) return;  // not started: no stack yet
  uintptr_t lo;
  if (s == G_RUNNING) {
    rt_m *m = g->m;
    if (!m || !m->stop_sp) rt_throw("gc: running goroutine has no stop point");
    lo = (uintptr_t)m->stop_sp;
  } else if (s == G_SYSCALL) {
    scan((uintptr_t)g->syscallregs, (uintptr_t)(g->syscallregs + 24));
    lo = (uintptr_t)g->syscallsp;
  } else {
    lo = (uintptr_t)g->sp;
  }
  if (lo < g->stack_lo || lo > g->stack_hi) rt_throw("gc: goroutine sp outside its stack");
  scan(lo, g->stack_hi);
}

// World stopped.
void sched_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi)) {
  uint32_t n = __atomic_load_n(&nallg, __ATOMIC_ACQUIRE);
  for (uint32_t c = 0; c * GCHUNK < n; c++) {
    gchunk *ch = gchunks[c];
    for (uint32_t w = 0; w < GCHUNK / 64; w++) {
      uint64_t bits = atomic_load_explicit(&ch->live[w], memory_order_relaxed);
      while (bits) {
        uint32_t b = (uint32_t)__builtin_ctzll(bits);
        bits &= bits - 1;
        scan_g(ch->g[w * 64 + b], scan);
      }
    }
  }
}

// ---- init ----

static uintptr_t parse_size(const char *s, uintptr_t def) {
  if (!s || !*s) return def;
  uintptr_t v = 0;
  for (; *s >= '0' && *s <= '9'; s++) v = v * 10 + (uintptr_t)(*s - '0');
  if (*s == 'k' || *s == 'K') v <<= 10;
  if (*s == 'm' || *s == 'M') v <<= 20;
  if (*s == 'g' || *s == 'G') v <<= 30;
  return v >= (16 << 10) ? v : def;
}

void sched_init(void) {
  fixalloc_init(&g_alloc, sizeof(rt_g));
  fixalloc_init(&m_alloc, sizeof(rt_m));
  fixalloc_init(&p_alloc, sizeof(rt_p));
  g_stacksize = parse_size(rt_getenv("MARMOT_STACKSIZE"), g_stacksize);
  main_stacksize = parse_size(rt_getenv("MARMOT_MAINSTACKSIZE"), main_stacksize);
  rt_m *m = &m0;
  setm(m);
#ifdef RT_TSAN
  m->g0.tsan_fiber = __tsan_get_current_fiber();
#endif
  m->thread = (void *)pthread_self();
  m->fastrand = rt_fastrand() | 1;
  sched.mcount = 1;
  sched.allm = m;
  int32_t n = os_ncpu();
  bool ok;
  int64_t env = rt_atoi(rt_getenv("GOMAXPROCS"), &ok);
  if (ok && env > 0) n = (int32_t)env;
  if (n > 256) n = 256;
  for (int32_t i = 0; i < n; i++) sched.allp[i] = pnew(i);
  atomic_store(&sched.nallp, n);
  atomic_store(&sched.nprocs, n);
  for (int32_t i = n - 1; i > 0; i--) pidleput(sched.allp[i]);
  acquirep(m, sched.allp[0]);
  signal_init();
  signal_init_thread(m);
  const char *dd = rt_getenv("MARMOT_DEADLOCK_DEBUG");
  if (dd && dd[0] && dd[0] != '0') watchdog_start();
}

static void main_entry(void *arg) { marmot_main(); }

void sched_start_main(void) {
  rt_m *m = getm();
  rt_g *g = galloc(main_stacksize);
  g->fn = main_entry;
  g->id = atomic_fetch_add(&sched.goidgen, 1) + 1;
  g->sp = rt_ctx_init((void *)g->stack_hi, gstart, g);
  glive_set(g, true);
  atomic_fetch_add(&sched.ngoroutines, 1);
  atomic_store(&g->status, G_RUNNABLE);
  rt_main_g = g;
  runqput(m->p, g, true);
  schedule_loop(m);
}

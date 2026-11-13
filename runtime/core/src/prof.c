// prof.c: CPU profile sampling for runtime/pprof.
//
// SIGPROF interrupts a thread that is using CPU: on linux each M has its
// own timer on its thread CPU clock (timer_create, SIGEV_THREAD_ID), so
// every thread is sampled in proportion to its CPU time; on darwin the
// process wide setitimer(ITIMER_PROF) is used. The handler records the interrupted pc and the return addresses of
// the frame pointer chain into a ring of fixed size records. The Go side
// (lib/src/runtime/profile.go) reads the ring in the record format of gc's
// profBuf, so the stock runtime/pprof writes the profile and symbolizes it
// through runtime.CallersFrames and the pc table.
//
// Record (PROF_WORDS words): [len, nanotime, count, pc0, pc1, ...]; len
// counts the words used. The handler allocates nothing and takes no locks:
// a writer reserves a slot with a CAS on the write position (dropping the
// sample when the reader is a whole ring behind) and publishes it by
// setting the slot's sequence number. Dropped samples are reported to the
// reader as an overflow record [4, 0, 0, n].

#include "internal.h"

#include <signal.h>
#include <sys/time.h>
#if RT_OS_LINUX
#include <pthread.h>
#include <time.h>
#endif

#define PROF_DEPTH 64
#define PROF_WORDS (3 + PROF_DEPTH)
#define PROF_SLOTS 4096

typedef struct {
  _Atomic uint64_t seq;  // index + 1 once written
  uint64_t w[PROF_WORDS];
} prof_slot;

static prof_slot *prof_ring;
static _Atomic uint64_t prof_wpos, prof_rpos, prof_lost;
static _Atomic int32_t prof_on;
static int32_t prof_installed;
static int32_t prof_hz;

#if RT_OS_LINUX
#ifndef sigev_notify_thread_id
#define sigev_notify_thread_id _sigev_un._tid
#endif

// prof_timer_start gives m a timer on its thread's CPU clock; self says
// that m is the calling thread (a new thread has no pthread_t stored yet).
// The starting thread and a new M may race to give m a timer: the timer is
// created first and claimed with a compare-exchange, and the loser deletes
// its own, so a thread never has two.
static void prof_timer_start(rt_m *m, bool self) {
  if (__atomic_load_n(&m->prof_timer, __ATOMIC_ACQUIRE) || m->tid == 0) return;
  clockid_t cid = CLOCK_THREAD_CPUTIME_ID;
  if (!self && (!m->thread || pthread_getcpuclockid((pthread_t)m->thread, &cid) != 0)) return;
  struct sigevent sev;
  __builtin_memset(&sev, 0, sizeof sev);
  sev.sigev_notify = SIGEV_THREAD_ID;
  sev.sigev_signo = SIGPROF;
  sev.sigev_notify_thread_id = m->tid;
  timer_t t;
  if (timer_create(cid, &sev, &t) != 0) return;
  struct itimerspec its;
  long ns = 1000000000L / prof_hz;
  its.it_interval.tv_sec = ns / 1000000000L;
  its.it_interval.tv_nsec = ns % 1000000000L;
  its.it_value = its.it_interval;
  void *none = NULL;
  if (!__atomic_compare_exchange_n(&m->prof_timer, &none, (void *)t, false, __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
    timer_delete(t);  // another thread gave m its timer first
    return;
  }
  if (timer_settime(t, 0, &its, NULL) != 0 || !atomic_load(&prof_on)) {
    // Failed, or profiling stopped meanwhile: take it back if still ours.
    void *mine = (void *)t;
    if (__atomic_compare_exchange_n(&m->prof_timer, &mine, NULL, false, __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) timer_delete(t);
  }
}

static void prof_timer_stop(rt_m *m) {
  void *t = __atomic_exchange_n(&m->prof_timer, NULL, __ATOMIC_ACQ_REL);
  if (t) timer_delete((timer_t)t);
}
#endif

void prof_thread_start(rt_m *m) {
#if RT_OS_LINUX
  if (atomic_load(&prof_on)) prof_timer_start(m, true);
#else
  (void)m;
#endif
}

// frame_ok checks a frame pointer before it is read: aligned, above the
// previous one, and inside the goroutine's stack when it is known.
static bool frame_ok(uintptr_t fp, uintptr_t prev, uintptr_t lo, uintptr_t hi) {
  if (fp == 0 || fp & 7 || fp <= prev) return false;
  if (lo && hi) return fp >= lo && fp + 16 <= hi;
  // Unknown bounds (g0, threads not running a goroutine): accept the first
  // frame, then steps of less than 1 MB.
  return prev == 0 || fp - prev < (1 << 20);
}

static void sigprof(int sig, siginfo_t *info, void *ctx) {
  (void)sig;
  (void)info;
  if (!atomic_load_explicit(&prof_on, memory_order_relaxed) || !prof_ring) return;
  ucontext_t *uc = ctx;
  uintptr_t pc = 0, fp = 0;
#if RT_OS_DARWIN && RT_ARCH_ARM64
  pc = uc->uc_mcontext->__ss.__pc;
  fp = uc->uc_mcontext->__ss.__fp;
#elif RT_OS_DARWIN && RT_ARCH_AMD64
  pc = uc->uc_mcontext->__ss.__rip;
  fp = uc->uc_mcontext->__ss.__rbp;
#elif RT_OS_LINUX && RT_ARCH_ARM64
  pc = uc->uc_mcontext.pc;
  fp = uc->uc_mcontext.regs[29];
#elif RT_OS_LINUX && RT_ARCH_AMD64
  pc = (uintptr_t)uc->uc_mcontext.gregs[REG_RIP];
  fp = (uintptr_t)uc->uc_mcontext.gregs[REG_RBP];
#endif
  // Reserve a slot; drop the sample when the reader is a ring behind.
  uint64_t w = atomic_load_explicit(&prof_wpos, memory_order_relaxed);
  for (;;) {
    if (w - atomic_load_explicit(&prof_rpos, memory_order_acquire) >= PROF_SLOTS) {
      atomic_fetch_add_explicit(&prof_lost, 1, memory_order_relaxed);
      return;
    }
    if (atomic_compare_exchange_weak_explicit(&prof_wpos, &w, w + 1, memory_order_acq_rel,
                                              memory_order_relaxed))
      break;
  }
  prof_slot *s = &prof_ring[w % PROF_SLOTS];
  uintptr_t lo = 0, hi = 0;
  rt_m *m = getm();
  if (m && m->curg) {
    lo = m->curg->stack_lo;
    hi = m->curg->stack_hi;
  }
  int n = 0;
  s->w[3 + n++] = pc;
  uintptr_t prev = 0;
  while (n < PROF_DEPTH && frame_ok(fp, prev, lo, hi)) {
    uintptr_t ret = ((uintptr_t *)fp)[1];
    if (ret == 0) break;
    s->w[3 + n++] = ret;
    prev = fp;
    fp = ((uintptr_t *)fp)[0];
  }
  s->w[0] = (uint64_t)(3 + n);
  s->w[1] = (uint64_t)os_nanotime();
  s->w[2] = 1;
  atomic_store_explicit(&s->seq, w + 1, memory_order_release);
}

RT_API int32_t rt_prof_start(int32_t hz) {
  if (hz <= 0) return -22;
  if (!prof_ring) {
    prof_ring = os_map(sizeof(prof_slot) * PROF_SLOTS);
    if (!prof_ring) return -12;
  }
  if (!prof_installed) {
    struct sigaction sa;
    __builtin_memset(&sa, 0, sizeof sa);
    sa.sa_sigaction = sigprof;
    sa.sa_flags = SA_SIGINFO | SA_RESTART | SA_ONSTACK;
    sigfillset(&sa.sa_mask);
    if (sigaction(SIGPROF, &sa, NULL) != 0) return -1;
    prof_installed = 1;
  }
  atomic_store(&prof_lost, 0);
  prof_hz = hz;
  atomic_store(&prof_on, 1);
#if RT_OS_LINUX
  // Ms only ever join the list; new threads start their timer themselves.
  rt_m *self = getm();
  for (rt_m *m = __atomic_load_n(&sched.allm, __ATOMIC_ACQUIRE); m; m = m->alllink) prof_timer_start(m, m == self);
  return 0;
#else
  struct itimerval it;
  it.it_interval.tv_sec = 0;
  it.it_interval.tv_usec = 1000000 / hz;
  if (it.it_interval.tv_usec == 0) it.it_interval.tv_usec = 1;
  it.it_value = it.it_interval;
  if (setitimer(ITIMER_PROF, &it, NULL) != 0) return -1;
  return 0;
#endif
}

RT_API void rt_prof_stop(void) {
#if RT_OS_LINUX
  atomic_store(&prof_on, 0);
  for (rt_m *m = __atomic_load_n(&sched.allm, __ATOMIC_ACQUIRE); m; m = m->alllink) prof_timer_stop(m);
#else
  struct itimerval it;
  __builtin_memset(&it, 0, sizeof it);
  setitimer(ITIMER_PROF, &it, NULL);
#endif
  // The handler stays installed so a signal still in flight is ignored.
  atomic_store(&prof_on, 0);
}

// rt_prof_read copies whole records into buf (n words) and returns the
// number of words copied: 0 when nothing is ready, -1 once profiling is
// stopped and everything has been read.
RT_API intptr_t rt_prof_read(uint64_t *buf, intptr_t n) {
  intptr_t k = 0;
  uint64_t lost = atomic_exchange(&prof_lost, 0);
  if (lost && n >= 4) {
    buf[0] = 4;
    buf[1] = 0;
    buf[2] = 0;
    buf[3] = lost;
    k = 4;
  } else if (lost) {
    atomic_fetch_add(&prof_lost, lost);
  }
  if (!prof_ring) return atomic_load(&prof_on) || k ? k : -1;
  uint64_t r = atomic_load_explicit(&prof_rpos, memory_order_relaxed);
  for (;;) {
    prof_slot *s = &prof_ring[r % PROF_SLOTS];
    if (atomic_load_explicit(&s->seq, memory_order_acquire) != r + 1) break;  // not written yet
    intptr_t len = (intptr_t)s->w[0];
    if (k + len > n) break;
    __builtin_memcpy(buf + k, s->w, (size_t)len * sizeof(uint64_t));
    k += len;
    r++;
    atomic_store_explicit(&prof_rpos, r, memory_order_release);
  }
  if (k == 0 && !atomic_load(&prof_on) && r == atomic_load(&prof_wpos)) return -1;
  return k;
}

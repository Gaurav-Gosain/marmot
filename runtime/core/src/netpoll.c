// netpoll.c: I/O poller. kqueue on darwin, epoll on linux (untested).
//
// The Go side makes an fd non-blocking, registers it with rt_poll_open, and
// when a read or write returns EAGAIN calls rt_poll_wait, which parks the
// goroutine until the fd is ready (edge triggered, like the gc runtime's
// netpoller), the deadline passes, or rt_poll_evict is called. The
// scheduler polls without blocking when it runs out of work, and one idle
// thread blocks in the poller (with a timeout for the next timer) while
// goroutines wait for I/O. sysmon polls if nobody has for 10 ms.

#include "internal.h"

#include <errno.h>
#include <unistd.h>
#if RT_OS_DARWIN
#include <sys/event.h>
#elif RT_OS_LINUX
#include <sys/epoll.h>
#include <sys/eventfd.h>
#endif

typedef struct polldesc {
  rt_spin lock;
  int32_t fd;
  bool closing;
  bool rready, wready;        // readiness seen while nobody waited
  bool rtimeout, wtimeout;
  bool rwoken, wwoken;        // rt_poll_wake
  rt_g *rg, *wg;              // waiting goroutines
  rt_timer *rt, *wt;          // deadline timers
  struct polldesc *next;      // free list
} polldesc;

static rt_spin pd_lock;
static polldesc *pd_free;
static int poll_fd = -1;
#if RT_OS_LINUX
static int break_fd = -1;
#endif
static rt_spin init_lock;
_Atomic int32_t netpoll_waiters;
_Atomic int32_t netpoll_blocked;
_Atomic int64_t netpoll_last;

static void netpoll_init(void) {
  if (__atomic_load_n(&poll_fd, __ATOMIC_ACQUIRE) >= 0) return;
  // Hook the poller into the scheduler (first descriptor).
  __atomic_store_n(&hook_netpoll_break, netpoll_break, __ATOMIC_RELEASE);
  __atomic_store_n(&hook_netpoll, netpoll, __ATOMIC_RELEASE);
  __atomic_store_n(&hook_sysmon_start, sysmon_start, __ATOMIC_RELEASE);
  sysmon_start();
  spin_lock(&init_lock);
  if (poll_fd < 0) {
#if RT_OS_DARWIN
    int kq = kqueue();
    if (kq < 0) rt_throw("netpoll: kqueue failed");
    struct kevent ev;
    EV_SET(&ev, 0, EVFILT_USER, EV_ADD | EV_CLEAR, 0, 0, NULL);
    if (kevent(kq, &ev, 1, NULL, 0, NULL) < 0) rt_throw("netpoll: cannot add user event");
    __atomic_store_n(&poll_fd, kq, __ATOMIC_RELEASE);
#elif RT_OS_LINUX
    int ep = epoll_create1(EPOLL_CLOEXEC);
    if (ep < 0) rt_throw("netpoll: epoll_create1 failed");
    break_fd = eventfd(0, EFD_NONBLOCK | EFD_CLOEXEC);
    struct epoll_event ev = {.events = EPOLLIN, .data.ptr = NULL};
    if (break_fd < 0 || epoll_ctl(ep, EPOLL_CTL_ADD, break_fd, &ev) < 0)
      rt_throw("netpoll: cannot set up eventfd");
    __atomic_store_n(&poll_fd, ep, __ATOMIC_RELEASE);
#endif
  }
  spin_unlock(&init_lock);
}

bool netpoll_inited(void) { return __atomic_load_n(&poll_fd, __ATOMIC_ACQUIRE) >= 0; }

// Interrupt a thread blocked in netpoll.
void netpoll_break(void) {
  if (!netpoll_inited()) return;
#if RT_OS_DARWIN
  struct kevent ev;
  EV_SET(&ev, 0, EVFILT_USER, 0, NOTE_TRIGGER, 0, NULL);
  kevent(poll_fd, &ev, 1, NULL, 0, NULL);
#elif RT_OS_LINUX
  uint64_t one = 1;
  ssize_t r = write(break_fd, &one, sizeof one);
  (void)r;
#endif
}

// Mark the descriptor ready for mode and wake its waiter.
static void pd_ready(polldesc *pd, bool read, bool write) {
  rt_g *r = NULL, *w = NULL;
  spin_lock(&pd->lock);
  if (read) {
    if (pd->rg) {
      r = pd->rg;
      pd->rg = NULL;
    } else {
      pd->rready = true;
    }
  }
  if (write) {
    if (pd->wg) {
      w = pd->wg;
      pd->wg = NULL;
    } else {
      pd->wready = true;
    }
  }
  spin_unlock(&pd->lock);
  if (r) rt_ready(r);
  if (w) rt_ready(w);
}

// Poll. ns < 0 blocks, 0 does not, > 0 waits at most ns. Returns the number
// of events handled.
int32_t netpoll(int64_t ns) {
  if (!netpoll_inited()) return 0;
  int32_t n = 0;
#if RT_OS_DARWIN
  struct kevent evs[64];
  struct timespec ts, *tsp = NULL;
  if (ns >= 0) {
    ts.tv_sec = ns / 1000000000;
    ts.tv_nsec = ns % 1000000000;
    tsp = &ts;
  }
  int k = kevent(poll_fd, NULL, 0, evs, 64, tsp);
  atomic_store(&netpoll_last, os_nanotime());
  for (int i = 0; i < k; i++) {
    if (evs[i].filter == EVFILT_USER) continue;
    polldesc *pd = evs[i].udata;
    if (!pd) continue;
    bool rd = evs[i].filter == EVFILT_READ;
    bool wr = evs[i].filter == EVFILT_WRITE;
    // EOF or error: wake both directions, the syscall reports it.
    if (evs[i].flags & (EV_EOF | EV_ERROR)) rd = wr = true;
    pd_ready(pd, rd, wr);
    n++;
  }
#elif RT_OS_LINUX
  struct epoll_event evs[64];
  int timeout = ns < 0 ? -1 : (int)((ns + 999999) / 1000000);
  int k = epoll_wait(poll_fd, evs, 64, timeout);
  atomic_store(&netpoll_last, os_nanotime());
  for (int i = 0; i < k; i++) {
    polldesc *pd = evs[i].data.ptr;
    if (!pd) {
      uint64_t v;
      ssize_t r = read(break_fd, &v, sizeof v);
      (void)r;
      continue;
    }
    uint32_t e = evs[i].events;
    bool rd = e & (EPOLLIN | EPOLLRDHUP | EPOLLHUP | EPOLLERR);
    bool wr = e & (EPOLLOUT | EPOLLHUP | EPOLLERR);
    pd_ready(pd, rd, wr);
    n++;
  }
#endif
  return n;
}

RT_API void *rt_poll_open(int32_t fd) {
  netpoll_init();
  spin_lock(&pd_lock);
  polldesc *pd = pd_free;
  if (pd) {
    pd_free = pd->next;
  }
  spin_unlock(&pd_lock);
  if (!pd) {
    pd = persistent_alloc(sizeof *pd, 16);
    pd->rt = itimer_new();
    pd->wt = itimer_new();
  }
  spin_lock(&pd->lock);
  pd->fd = fd;
  pd->closing = false;
  pd->rready = pd->wready = false;
  pd->rtimeout = pd->wtimeout = false;
  pd->rg = pd->wg = NULL;
  pd->next = NULL;
  spin_unlock(&pd->lock);
#if RT_OS_DARWIN
  struct kevent ev[2];
  EV_SET(&ev[0], fd, EVFILT_READ, EV_ADD | EV_CLEAR, 0, 0, pd);
  EV_SET(&ev[1], fd, EVFILT_WRITE, EV_ADD | EV_CLEAR, 0, 0, pd);
  if (kevent(poll_fd, ev, 2, NULL, 0, NULL) < 0) {
#elif RT_OS_LINUX
  struct epoll_event ev = {.events = EPOLLIN | EPOLLOUT | EPOLLRDHUP | EPOLLET, .data.ptr = pd};
  if (epoll_ctl(poll_fd, EPOLL_CTL_ADD, fd, &ev) < 0) {
#endif
    spin_lock(&pd_lock);
    pd->next = pd_free;
    pd_free = pd;
    spin_unlock(&pd_lock);
    return NULL;
  }
  return pd;
}

RT_API void rt_poll_evict(void *h) {
  polldesc *pd = h;
  spin_lock(&pd->lock);
  pd->closing = true;
  rt_g *r = pd->rg, *w = pd->wg;
  pd->rg = pd->wg = NULL;
  spin_unlock(&pd->lock);
  if (r) rt_ready(r);
  if (w) rt_ready(w);
}

RT_API void rt_poll_wake(void *h, int32_t mode) {
  polldesc *pd = h;
  rt_g *g = NULL;
  spin_lock(&pd->lock);
  if (mode == 'r' && pd->rg) {
    g = pd->rg;
    pd->rg = NULL;
    pd->rwoken = true;
  } else if (mode == 'w' && pd->wg) {
    g = pd->wg;
    pd->wg = NULL;
    pd->wwoken = true;
  }
  spin_unlock(&pd->lock);
  if (g) rt_ready(g);
}

RT_API void rt_poll_close(void *h) {
  polldesc *pd = h;
  rt_poll_evict(pd);
#if RT_OS_DARWIN
  struct kevent ev[2];
  EV_SET(&ev[0], pd->fd, EVFILT_READ, EV_DELETE, 0, 0, NULL);
  EV_SET(&ev[1], pd->fd, EVFILT_WRITE, EV_DELETE, 0, 0, NULL);
  kevent(poll_fd, ev, 2, NULL, 0, NULL);
#elif RT_OS_LINUX
  struct epoll_event ev;
  epoll_ctl(poll_fd, EPOLL_CTL_DEL, pd->fd, &ev);
#endif
  spin_lock(&pd_lock);
  pd->next = pd_free;
  pd_free = pd;
  spin_unlock(&pd_lock);
}

static void deadline_read(void *arg) {
  polldesc *pd = arg;
  spin_lock(&pd->lock);
  rt_g *g = pd->rg;
  if (g) {
    pd->rg = NULL;
    pd->rtimeout = true;
  }
  spin_unlock(&pd->lock);
  if (g) rt_ready(g);
}

static void deadline_write(void *arg) {
  polldesc *pd = arg;
  spin_lock(&pd->lock);
  rt_g *g = pd->wg;
  if (g) {
    pd->wg = NULL;
    pd->wtimeout = true;
  }
  spin_unlock(&pd->lock);
  if (g) rt_ready(g);
}

static void pd_unlock(void *pd) { spin_unlock(&((polldesc *)pd)->lock); }

RT_API int32_t rt_poll_wait(void *h, int32_t mode, int64_t deadline) {
  polldesc *pd = h;
  bool rd = mode == 'r';
  spin_lock(&pd->lock);
  for (;;) {
    if (pd->closing) {
      spin_unlock(&pd->lock);
      return 2;
    }
    bool *ready = rd ? &pd->rready : &pd->wready;
    if (*ready) {
      *ready = false;
      spin_unlock(&pd->lock);
      return 0;
    }
    if (deadline > 0 && os_nanotime() >= deadline) {
      spin_unlock(&pd->lock);
      return 1;
    }
    if (rd) {
      pd->rg = rt_current();
      pd->rtimeout = false;
      pd->rwoken = false;
    } else {
      pd->wg = rt_current();
      pd->wtimeout = false;
      pd->wwoken = false;
    }
    rt_timer *t = rd ? pd->rt : pd->wt;
    if (deadline > 0) itimer_start(t, deadline, rd ? deadline_read : deadline_write, pd);
    atomic_fetch_add(&netpoll_waiters, 1);
    // A thread may be blocked in the poller without this fd; it will see the
    // new waiter count on its next poll. Make sure someone polls.
    if (atomic_load(&netpoll_blocked) == 0) wakep();
    rt_park(pd_unlock, pd);
    atomic_fetch_sub(&netpoll_waiters, 1);
    if (deadline > 0) itimer_stop(t);
    spin_lock(&pd->lock);
    bool timedout = rd ? pd->rtimeout : pd->wtimeout;
    if (timedout) {
      spin_unlock(&pd->lock);
      return 1;
    }
    if (rd ? pd->rwoken : pd->wwoken) {
      spin_unlock(&pd->lock);
      return 3;
    }
    // Woken by readiness or eviction: loop to report it. Readiness was
    // handed to us directly (the flag stays clear), so return ready unless
    // closing.
    if (pd->closing) {
      spin_unlock(&pd->lock);
      return 2;
    }
    spin_unlock(&pd->lock);
    return 0;
  }
}

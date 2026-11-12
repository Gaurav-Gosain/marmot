// signal.c: signal handling.
//
// SIGURG stops a running M for stop-the-world when it does not reach a
// safepoint by itself (tight loops). SIGSEGV and SIGBUS detect stack overflow
// (guard page) and turn faults in goroutine code into calls to the sigpanic
// hook set by the Go runtime, or a fatal error if none is set. SIGPIPE is
// ignored; rt_write raises it for fds 1 and 2 like the gc runtime.

#include "internal.h"
#include "malloc.h"

#include <errno.h>
#include <signal.h>
#include <pthread.h>
#if RT_OS_LINUX
#include <ucontext.h>
#endif

static void (*sigpanic_fn)(uintptr_t addr, int32_t kind);

RT_API void rt_set_sigpanic(void (*fn)(uintptr_t addr, int32_t kind)) { sigpanic_fn = fn; }

// ---- signals for the Go side (os/signal) ----

static _Atomic uint64_t sig_pending;  // bit n: signal n arrived
static _Atomic uint64_t sig_enabled;  // bit n: queue signal n for Go
static uint64_t sig_ignored;          // bit n: ignored (inherited or rt_signal_ignore)
static rt_spin sig_lock;
static rt_g *sig_waiter;
static _Atomic int32_t sig_nwaiting;

// Signals whose default action ends the process. When the Go side asked
// for one (signal.Notify) but no goroutine takes it, the process must still
// end: a second arrival while the first is pending, or the first pending for
// 3 seconds (sysmon checks, signal_check_stuck), restores the default action
// and raises it. A healthy program takes a signal within milliseconds.
#define SIG_TERMINATING ((1ull << SIGTERM) | (1ull << SIGINT) | (1ull << SIGHUP) | (1ull << SIGQUIT))
static _Atomic int64_t sig_since[64];  // monotonic time a terminating signal became pending

static void sig_die(int sig) {
  signal(sig, SIG_DFL);
  sigset_t set;
  sigemptyset(&set);
  sigaddset(&set, sig);
  pthread_sigmask(SIG_UNBLOCK, &set, NULL);
  raise(sig);
  os_exit(128 + sig);  // not reached for a terminating signal
}

static void sig_queue(int sig, siginfo_t *info, void *ctx) {
  int saved = errno;
  uint64_t bit = 1ull << sig;
  uint64_t old = atomic_fetch_or(&sig_pending, bit);
  if (bit & SIG_TERMINATING) {
    if (old & bit) sig_die(sig);  // second one while the first waits: give up on the Go side
    atomic_store(&sig_since[sig], os_nanotime());
  }
  sysmon_kick();
  errno = saved;
}

// sysmon, every round: terminating signals nobody took for 3 seconds.
void signal_check_stuck(void) {
  uint64_t p = atomic_load(&sig_pending) & SIG_TERMINATING;
  if (!p) return;
  int64_t now = os_nanotime();
  while (p) {
    int sig = __builtin_ctzll(p);
    p &= p - 1;
    int64_t t = atomic_load(&sig_since[sig]);
    if (t && now - t > 3000000000LL && (atomic_load(&sig_pending) & (1ull << sig))) {
      rt_print("marmot: signal ");
      rt_printi(sig);
      rt_print(" not received by the program for 3 s (scheduler stuck?): default action\n");
      sig_die(sig);
    }
  }
}

bool signal_waiting(void) { return atomic_load(&sig_nwaiting) > 0; }

bool signal_deliver(void) {
  if (!atomic_load(&sig_pending) || !atomic_load(&sig_nwaiting)) return false;
  spin_lock(&sig_lock);
  rt_g *g = sig_waiter;
  sig_waiter = NULL;
  spin_unlock(&sig_lock);
  if (g) rt_ready(g);
  return g != NULL;
}

static bool sig_runtime_owned(int sig) {
  return sig == SIGURG || sig == SIGSEGV || sig == SIGBUS || sig == SIGPIPE;
}

static void install(int sig, void (*fn)(int, siginfo_t *, void *), int flags);
static void sigurg(int sig, siginfo_t *info, void *ctx);

RT_API int32_t rt_signal_enable(int32_t sig) {
  if (sig <= 0 || sig >= 64 || sig == SIGKILL || sig == SIGSTOP) return -EINVAL;
  sysmon_start();
  atomic_fetch_or(&sig_enabled, 1ull << sig);
  sig_ignored &= ~(1ull << sig);
  if (sig == SIGURG || sig == SIGSEGV || sig == SIGBUS) return 0;  // runtime handler forwards
  install(sig, sig_queue, SA_ONSTACK);
  return 0;
}

// SIGPIPE: a handler that does nothing, as gc: writes get EPIPE (rt_write
// and rt_sigpipe turn EPIPE on fd 1 and 2 into a real SIGPIPE), and unlike
// SIG_IGN a handler is reset to the default by exec, so child processes
// still die from SIGPIPE. Ignored at startup (nohup): stays ignored.
static void sigpipe_noop(int sig, siginfo_t *info, void *ctx) {
  (void)sig;
  (void)info;
  (void)ctx;
}

static void sigpipe_default(void) {
  if (sig_ignored & (1ull << SIGPIPE))
    signal(SIGPIPE, SIG_IGN);
  else
    install(SIGPIPE, sigpipe_noop, SA_ONSTACK);
}

static void sig_default(int sig) {
  if (sig == SIGURG) return;
  if (sig == SIGPIPE) {
    sigpipe_default();
    return;
  }
  if (sig == SIGSEGV || sig == SIGBUS) return;
  signal(sig, SIG_DFL);
}

RT_API int32_t rt_signal_disable(int32_t sig) {
  if (sig <= 0 || sig >= 64) return -EINVAL;
  atomic_fetch_and(&sig_enabled, ~(1ull << sig));
  sig_default(sig);
  return 0;
}

RT_API int32_t rt_signal_ignore(int32_t sig) {
  if (sig <= 0 || sig >= 64 || sig == SIGKILL || sig == SIGSTOP) return -EINVAL;
  atomic_fetch_and(&sig_enabled, ~(1ull << sig));
  sig_ignored |= 1ull << sig;
  if (!sig_runtime_owned(sig) || sig == SIGPIPE) signal(sig, SIG_IGN);
  return 0;
}

RT_API int32_t rt_signal_ignored(int32_t sig) {
  if (sig <= 0 || sig >= 64) return 0;
  return (sig_ignored >> sig) & 1;
}

static void sig_unlock(void *unused) { spin_unlock(&sig_lock); }

RT_API int32_t rt_signal_recv(void) {
  for (;;) {
    uint64_t p = atomic_load(&sig_pending);
    while (p) {
      int b = __builtin_ctzll(p);
      if (atomic_compare_exchange_weak(&sig_pending, &p, p & ~(1ull << b))) return b;
    }
    spin_lock(&sig_lock);
    if (atomic_load(&sig_pending)) {
      spin_unlock(&sig_lock);
      continue;
    }
    sig_waiter = rt_current();
    atomic_fetch_add(&sig_nwaiting, 1);
    rt_park(sig_unlock, NULL);
    atomic_fetch_sub(&sig_nwaiting, 1);
  }
}

static void sigurg(int sig, siginfo_t *info, void *ctx) {
  int saved = errno;
  if (atomic_load(&sig_enabled) & (1ull << SIGURG)) {
    atomic_fetch_or(&sig_pending, 1ull << SIGURG);
    sysmon_kick();
  }
  rt_m *m = getm();
  if (m) {
    atomic_fetch_add_explicit(&m->nsigurg, 1, memory_order_relaxed);
    int why = !atomic_load(&sched.gcwaiting) ? 1
              : m->locks != 0                ? 2
              : !m->curg                     ? 3
              : !m->p                        ? 4
              : atomic_load(&m->p->status) != P_RUNNING ? 5
              : m->p->m != m                 ? 6
                                             : 0;
    if (why == 0) {
      rt_g *g = m->curg;
      uintptr_t sp = (uintptr_t)__builtin_frame_address(0);
      // Only stop when running on the goroutine stack: the signal frame,
      // with all registers of the interrupted code, is then above sp.
      if (sp > g->stack_lo && sp < g->stack_hi) {
        atomic_fetch_add_explicit(&m->nsigstop, 1, memory_order_relaxed);
        sched_stop_self((void *)sp, g->stack_hi);
      } else {
        why = 7;
      }
    }
    atomic_store_explicit(&m->sigurg_why, why, memory_order_relaxed);
  }
  errno = saved;
}

void signal_preempt(rt_m *m) {
  if (m->thread) pthread_kill((pthread_t)m->thread, SIGURG);
}

static void fault_print(const char *what, int sig, uintptr_t addr, uintptr_t pc) {
  rt_print(what);
  rt_print("\n[signal ");
  rt_print(sig == SIGBUS ? "SIGBUS: bus error" : "SIGSEGV: segmentation violation");
  rt_print(" addr=");
  rt_printx(addr);
  rt_print(" pc=");
  rt_printx(pc);
  rt_print("]\n");
}

static void sigfault(int sig, siginfo_t *info, void *ctxp) {
  ucontext_t *uc = ctxp;
  uintptr_t addr = (uintptr_t)info->si_addr;
  uintptr_t pc;
#if RT_OS_DARWIN && RT_ARCH_ARM64
  pc = uc->uc_mcontext->__ss.__pc;
#elif RT_OS_DARWIN && RT_ARCH_AMD64
  pc = uc->uc_mcontext->__ss.__rip;
#elif RT_OS_LINUX && RT_ARCH_ARM64
  pc = uc->uc_mcontext.pc;
#elif RT_OS_LINUX && RT_ARCH_AMD64
  pc = (uintptr_t)uc->uc_mcontext.gregs[REG_RIP];
#endif
  // MARMOT_POISON: an access to freed memory is fatal, never a Go panic.
  if (poison_fault(addr, pc)) os_exit(2);
  rt_m *m = getm();
  rt_g *g = m ? m->curg : NULL;
  // Stack overflow: the address is in a goroutine's guard page.
  if (g && addr >= g->map_base && addr < g->stack_lo) {
    if (!rt_output_json()) {
      rt_print("runtime: goroutine stack exceeds ");
      rt_printu(g->stack_hi - g->stack_lo);
      rt_print("-byte limit\n");
    }
    rt_throw("stack overflow");
  }
  // A fault in goroutine code (not inside the runtime core) becomes a Go
  // panic through the hook, as if the faulting instruction called it.
  if (g && sigpanic_fn && m->locks == 0) {
    int32_t kind = addr < 0x10000 ? 1 : 2;
#if RT_OS_DARWIN && RT_ARCH_ARM64
    uc->uc_mcontext->__ss.__x[0] = addr;
    uc->uc_mcontext->__ss.__x[1] = (uint64_t)kind;
    uc->uc_mcontext->__ss.__lr = pc + 4;
    uc->uc_mcontext->__ss.__pc = (uint64_t)sigpanic_fn;
#elif RT_OS_DARWIN && RT_ARCH_AMD64
    uc->uc_mcontext->__ss.__rdi = addr;
    uc->uc_mcontext->__ss.__rsi = (uint64_t)kind;
    uc->uc_mcontext->__ss.__rsp -= 8;
    *(uint64_t *)uc->uc_mcontext->__ss.__rsp = pc;
    uc->uc_mcontext->__ss.__rip = (uint64_t)sigpanic_fn;
#elif RT_OS_LINUX && RT_ARCH_ARM64
    uc->uc_mcontext.regs[0] = addr;
    uc->uc_mcontext.regs[1] = (uint64_t)kind;
    uc->uc_mcontext.regs[30] = pc + 4;
    uc->uc_mcontext.pc = (uint64_t)sigpanic_fn;
#elif RT_OS_LINUX && RT_ARCH_AMD64
    uc->uc_mcontext.gregs[REG_RDI] = (greg_t)addr;
    uc->uc_mcontext.gregs[REG_RSI] = kind;
    uc->uc_mcontext.gregs[REG_RSP] -= 8;
    *(uint64_t *)uc->uc_mcontext.gregs[REG_RSP] = pc;
    uc->uc_mcontext.gregs[REG_RIP] = (greg_t)sigpanic_fn;
#endif
    return;
  }
  if (addr < 0x10000) {
    fault_print("panic: runtime error: invalid memory address or nil pointer dereference", sig,
                addr, pc);
  } else {
    fault_print("unexpected fault address", sig, addr, pc);
    rt_print("fatal error: fault\n");
  }
  os_exit(2);
}

static void install(int sig, void (*fn)(int, siginfo_t *, void *), int flags) {
  struct sigaction sa;
  __builtin_memset(&sa, 0, sizeof sa);
  sa.sa_sigaction = fn;
  sa.sa_flags = SA_SIGINFO | SA_RESTART | flags;
  sigfillset(&sa.sa_mask);
  sigaction(sig, &sa, NULL);
}

void signal_init(void) {
  // Remember signals ignored at startup (nohup and friends).
  for (int sig = 1; sig < 32; sig++) {
    struct sigaction old;
    if (sigaction(sig, NULL, &old) == 0 && old.sa_handler == SIG_IGN) sig_ignored |= 1ull << sig;
  }
  install(SIGURG, sigurg, 0);
  install(SIGSEGV, sigfault, SA_ONSTACK);
  install(SIGBUS, sigfault, SA_ONSTACK);
  sigpipe_default();
}

void signal_init_thread(rt_m *m) {
  uintptr_t size = 64 << 10;
  void *p = os_map(size);
  if (!p) rt_throw("cannot allocate signal stack");
  stack_t ss;
  ss.ss_sp = p;
  ss.ss_size = size;
  ss.ss_flags = 0;
  sigaltstack(&ss, NULL);
  m->sigstack = (uintptr_t)p;
  // Make sure SIGURG is not blocked in this thread.
  sigset_t set;
  sigemptyset(&set);
  sigaddset(&set, SIGURG);
  pthread_sigmask(SIG_UNBLOCK, &set, NULL);
}

// Called by rt_write on EPIPE for stdout or stderr: die by SIGPIPE.
RT_API void rt_sigpipe(void) { signal_raise_sigpipe(); }

void signal_raise_sigpipe(void) {
  if (atomic_load(&sig_enabled) & (1ull << SIGPIPE)) {
    atomic_fetch_or(&sig_pending, 1ull << SIGPIPE);
    sysmon_kick();
    return;
  }
  if (sig_ignored & (1ull << SIGPIPE)) return;
  signal(SIGPIPE, SIG_DFL);
  sigset_t set;
  sigemptyset(&set);
  sigaddset(&set, SIGPIPE);
  pthread_sigmask(SIG_UNBLOCK, &set, NULL);
  raise(SIGPIPE);
}

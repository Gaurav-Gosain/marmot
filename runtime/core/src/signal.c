// signal.c: signal handling.
//
// SIGURG stops a running M for stop-the-world when it does not reach a
// safepoint by itself (tight loops). SIGSEGV and SIGBUS detect stack overflow
// (guard page) and turn faults in goroutine code into calls to the sigpanic
// hook set by the Go runtime, or a fatal error if none is set. SIGPIPE is
// ignored; rt_write raises it for fds 1 and 2 like the gc runtime.

#include "internal.h"

#include <errno.h>
#include <signal.h>
#include <pthread.h>
#if RT_OS_LINUX
#include <ucontext.h>
#endif

static void (*sigpanic_fn)(uintptr_t addr, int32_t kind);

RT_API void rt_set_sigpanic(void (*fn)(uintptr_t addr, int32_t kind)) { sigpanic_fn = fn; }

static void sigurg(int sig, siginfo_t *info, void *ctx) {
  int saved = errno;
  rt_m *m = getm();
  if (m && atomic_load(&sched.gcwaiting) && m->locks == 0 && m->curg && m->p &&
      atomic_load(&m->p->status) == P_RUNNING && m->p->m == m) {
    rt_g *g = m->curg;
    uintptr_t sp = (uintptr_t)__builtin_frame_address(0);
    // Only stop when running on the goroutine stack: the signal frame, with
    // all registers of the interrupted code, is then above sp.
    if (sp > g->stack_lo && sp < g->stack_hi) sched_stop_self((void *)sp, g->stack_hi);
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
  rt_m *m = getm();
  rt_g *g = m ? m->curg : NULL;
  // Stack overflow: the address is in a goroutine's guard page.
  if (g && addr >= g->map_base && addr < g->stack_lo) {
    rt_print("runtime: goroutine stack exceeds ");
    rt_printu(g->stack_hi - g->stack_lo);
    rt_print("-byte limit\n");
    rt_print("fatal error: stack overflow\n");
    os_exit(2);
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
  install(SIGURG, sigurg, 0);
  install(SIGSEGV, sigfault, SA_ONSTACK);
  install(SIGBUS, sigfault, SA_ONSTACK);
  signal(SIGPIPE, SIG_IGN);
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
void signal_raise_sigpipe(void) {
  signal(SIGPIPE, SIG_DFL);
  sigset_t set;
  sigemptyset(&set);
  sigaddset(&set, SIGPIPE);
  pthread_sigmask(SIG_UNBLOCK, &set, NULL);
  raise(SIGPIPE);
}

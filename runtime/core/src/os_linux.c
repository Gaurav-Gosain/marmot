// os_linux.c: OS layer for linux (amd64 and arm64).
// Status: linux/amd64 tested on a remote machine; linux/arm64 compile-checked only.

#include "internal.h"

#if RT_OS_LINUX

#include <errno.h>
#include <link.h>
#include <linux/futex.h>
#include <pthread.h>
#include <sched.h>
#include <stdlib.h>
#include <sys/mman.h>
#include <sys/syscall.h>
#include <time.h>
#include <unistd.h>

uintptr_t rt_pagesize;
uintptr_t rt_m_tlsoff;
// Static TLS: its offset from the thread pointer is the same in every thread.
__attribute__((tls_model("initial-exec"))) _Thread_local rt_m *rt_tls_m;

static uintptr_t thread_pointer(void) {
  uintptr_t tp;
#if RT_ARCH_AMD64
  __asm__ volatile("movq %%fs:0, %0" : "=r"(tp));
#else
  __asm__ volatile("mrs %0, tpidr_el0" : "=r"(tp));
#endif
  return tp;
}

void tls_init(void) {
  rt_m_tlsoff = (uintptr_t)&rt_tls_m - thread_pointer();
  rt_tls_m = NULL;
  if (getm() != NULL) {
    static const char msg[] = "fatal error: unexpected TLS layout\n";
    os_write_err(msg, sizeof msg - 1);
    os_exit(2);
  }
}

void setm(rt_m *m) { rt_tls_m = m; }

void os_init(void) {
  tls_init();
  long ps = sysconf(_SC_PAGESIZE);
  rt_pagesize = ps > 0 ? (uintptr_t)ps : 4096;
}

void *os_reserve(uintptr_t n) {
  void *p = mmap(NULL, n, PROT_NONE, MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1, 0);
  return p == MAP_FAILED ? NULL : p;
}

bool os_commit(void *p, uintptr_t n) { return mprotect(p, n, PROT_READ | PROT_WRITE) == 0; }

void *os_map(uintptr_t n) {
  void *p = mmap(NULL, n, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1,
                 0);
  return p == MAP_FAILED ? NULL : p;
}

void os_unmap(void *p, uintptr_t n) { munmap(p, n); }

// Only whole pages inside [p, p+n). Reads afterwards return zero.
void os_release(void *p, uintptr_t n) {
  uintptr_t lo = ((uintptr_t)p + rt_pagesize - 1) & ~(rt_pagesize - 1);
  uintptr_t hi = ((uintptr_t)p + n) & ~(rt_pagesize - 1);
  if (hi > lo) madvise((void *)lo, hi - lo, MADV_DONTNEED);
}

void os_protect_none(void *p, uintptr_t n) { mprotect(p, n, PROT_NONE); }

int32_t os_ncpu(void) {
  cpu_set_t set;
  if (sched_getaffinity(0, sizeof set, &set) == 0) {
    int n = CPU_COUNT(&set);
    if (n > 0) return n;
  }
  long n = sysconf(_SC_NPROCESSORS_ONLN);
  return n > 0 ? (int32_t)n : 1;
}

int64_t os_nanotime(void) {
  struct timespec ts;
  clock_gettime(CLOCK_MONOTONIC, &ts);
  return (int64_t)ts.tv_sec * 1000000000 + ts.tv_nsec;
}

void os_walltime(int64_t *sec, int32_t *nsec) {
  struct timespec ts;
  clock_gettime(CLOCK_REALTIME, &ts);
  *sec = ts.tv_sec;
  *nsec = (int32_t)ts.tv_nsec;
}

void os_write_err(const void *buf, uintptr_t n) {
  const char *p = buf;
  while (n > 0) {
    ssize_t r = write(2, p, n);
    if (r < 0) {
      if (errno == EINTR) continue;
      return;
    }
    p += r;
    n -= (uintptr_t)r;
  }
}

// PGO: an instrumented build links the LLVM profile runtime, whose strong
// __llvm_profile_write_file replaces this weak no-op; it normally writes the
// profile from atexit, which _exit skips, so write it here. Normal builds
// call the no-op.
__attribute__((weak)) int __llvm_profile_write_file(void) { return 0; }

void os_exit(int32_t code) {
  __llvm_profile_write_file();
  _exit(code);
}

void os_futex_wait(_Atomic uint32_t *addr, uint32_t val, int64_t ns) {
  struct timespec ts, *tsp = NULL;
  if (ns >= 0) {
    ts.tv_sec = ns / 1000000000;
    ts.tv_nsec = ns % 1000000000;
    tsp = &ts;
  }
  syscall(SYS_futex, (void *)addr, FUTEX_WAIT_PRIVATE, val, tsp, NULL, 0);
}

void os_futex_wake(_Atomic uint32_t *addr, bool all) {
  syscall(SYS_futex, (void *)addr, FUTEX_WAKE_PRIVATE, all ? INT32_MAX : 1, NULL, NULL, 0);
}

void os_usleep(uint32_t us) { usleep(us); }

void os_yield(void) { sched_yield(); }

static void (*seg_fn)(void *, void *);

static int phdr_cb(struct dl_phdr_info *info, size_t size, void *data) {
  // The first object is the main program.
  for (int i = 0; i < info->dlpi_phnum; i++) {
    const ElfW(Phdr) *ph = &info->dlpi_phdr[i];
    if (ph->p_type == PT_LOAD && (ph->p_flags & PF_W)) {
      uintptr_t start = info->dlpi_addr + ph->p_vaddr;
      seg_fn((void *)start, (void *)(start + ph->p_memsz));
    }
  }
  return 1;  // stop after the main program
}

void os_data_segments(void (*fn)(void *start, void *end)) {
  seg_fn = fn;
  dl_iterate_phdr(phdr_cb, NULL);
}

typedef struct {
  void (*fn)(void *);
  void *arg;
} thread_start;

static void *thread_entry(void *p) {
  thread_start ts = *(thread_start *)p;
  free(p);
  ts.fn(ts.arg);
  return NULL;
}

bool os_thread_create(void (*fn)(void *), void *arg, uintptr_t stacksize) {
  pthread_attr_t attr;
  pthread_attr_init(&attr);
  pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_DETACHED);
  if (stacksize) pthread_attr_setstacksize(&attr, stacksize);
  thread_start *ts = malloc(sizeof *ts);
  if (!ts) return false;
  ts->fn = fn;
  ts->arg = arg;
  pthread_t t;
  int r = pthread_create(&t, &attr, thread_entry, ts);
  pthread_attr_destroy(&attr);
  if (r != 0) {
    free(ts);
    return false;
  }
  return true;
}

intptr_t os_syscall6(uintptr_t num, uintptr_t a1, uintptr_t a2, uintptr_t a3, uintptr_t a4,
                     uintptr_t a5, uintptr_t a6) {
  long r = syscall((long)num, a1, a2, a3, a4, a5, a6);
  if (r == -1) return -(intptr_t)errno;
  return (intptr_t)r;
}

#endif

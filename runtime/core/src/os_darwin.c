// os_darwin.c: OS layer for darwin (arm64 tested, amd64 written and untested).

#include "internal.h"

#if RT_OS_DARWIN

#include <errno.h>
#include <stdlib.h>
#include <mach-o/dyld.h>
#include <mach-o/getsect.h>
#include <mach-o/loader.h>
#include <mach/mach_time.h>
#include <pthread.h>
#include <sched.h>
#include <sys/mman.h>
#include <sys/sysctl.h>
#include <time.h>
#include <unistd.h>

// Private but stable since macOS 11; used by libc++ and Swift.
extern int __ulock_wait2(uint32_t op, void *addr, uint64_t value, uint64_t timeout_ns,
                         uint64_t value2);
extern int __ulock_wake(uint32_t op, void *addr, uint64_t wake_value);
#define UL_COMPARE_AND_WAIT 1
#define ULF_WAKE_ALL 0x00000100
#define ULF_NO_ERRNO 0x01000000

uintptr_t rt_pagesize;
static uint32_t tb_numer, tb_denom;

uintptr_t rt_m_tlsoff;
_Thread_local rt_m *rt_tls_m;  // unused on darwin
static pthread_key_t m_key;

void tls_init(void) {
  if (pthread_key_create(&m_key, NULL) != 0) os_exit(2);
  rt_m_tlsoff = (uintptr_t)m_key * sizeof(void *);
  // The fast path reads the slot directly; make sure that matches.
  pthread_setspecific(m_key, (void *)&m_key);
  if ((void *)getm() != (void *)&m_key) {
    static const char msg[] = "fatal error: unexpected TSD layout\n";
    os_write_err(msg, sizeof msg - 1);
    os_exit(2);
  }
  pthread_setspecific(m_key, NULL);
}

void setm(rt_m *m) { pthread_setspecific(m_key, m); }

void os_init(void) {
  tls_init();
  rt_pagesize = (uintptr_t)getpagesize();
  mach_timebase_info_data_t tb;
  mach_timebase_info(&tb);
  tb_numer = tb.numer;
  tb_denom = tb.denom;
}

void *os_reserve(uintptr_t n) {
  void *p = mmap(NULL, n, PROT_NONE, MAP_PRIVATE | MAP_ANON, -1, 0);
  return p == MAP_FAILED ? NULL : p;
}

bool os_commit(void *p, uintptr_t n) {
  return mprotect(p, n, PROT_READ | PROT_WRITE) == 0;
}

void *os_map(uintptr_t n) {
  void *p = mmap(NULL, n, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANON, -1, 0);
  return p == MAP_FAILED ? NULL : p;
}

void os_unmap(void *p, uintptr_t n) { munmap(p, n); }

// Only whole OS pages inside [p, p+n) are released: the kernel would round a
// partial page outward and discard live neighbors.
void os_release(void *p, uintptr_t n) {
  uintptr_t lo = ((uintptr_t)p + rt_pagesize - 1) & ~(rt_pagesize - 1);
  uintptr_t hi = ((uintptr_t)p + n) & ~(rt_pagesize - 1);
  if (hi > lo) madvise((void *)lo, hi - lo, MADV_FREE_REUSABLE);
}

void os_protect_none(void *p, uintptr_t n) { mprotect(p, n, PROT_NONE); }

int32_t os_ncpu(void) {
  int n = 0;
  size_t len = sizeof n;
  if (sysctlbyname("hw.activecpu", &n, &len, NULL, 0) != 0 || n < 1) n = 1;
  return n;
}

int64_t os_nanotime(void) {
  uint64_t t = mach_absolute_time();
  if (tb_numer == tb_denom) return (int64_t)t;
  // Apple silicon is 125/3; split to avoid overflow.
  return (int64_t)((t / tb_denom) * tb_numer + (t % tb_denom) * tb_numer / tb_denom);
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

void os_exit(int32_t code) { _exit(code); }

void os_futex_wait(_Atomic uint32_t *addr, uint32_t val, int64_t ns) {
  uint64_t timeout = ns < 0 ? 0 : (ns == 0 ? 1 : (uint64_t)ns);
  __ulock_wait2(UL_COMPARE_AND_WAIT | ULF_NO_ERRNO, (void *)addr, val, timeout, 0);
}

void os_futex_wake(_Atomic uint32_t *addr, bool all) {
  __ulock_wake(UL_COMPARE_AND_WAIT | ULF_NO_ERRNO | (all ? ULF_WAKE_ALL : 0), (void *)addr, 0);
}

void os_usleep(uint32_t us) { usleep(us); }

void os_yield(void) { sched_yield(); }

void os_data_segments(void (*fn)(void *start, void *end)) {
  const struct mach_header_64 *mh = (const struct mach_header_64 *)_dyld_get_image_header(0);
  intptr_t slide = _dyld_get_image_vmaddr_slide(0);
  const struct load_command *lc = (const struct load_command *)(mh + 1);
  for (uint32_t i = 0; i < mh->ncmds; i++) {
    if (lc->cmd == LC_SEGMENT_64) {
      const struct segment_command_64 *seg = (const struct segment_command_64 *)lc;
      // __DATA, __DATA_DIRTY and friends: writable segments. __DATA_CONST
      // holds only link-time constants and is skipped.
      if ((seg->initprot & VM_PROT_WRITE) && seg->vmsize > 0 &&
          __builtin_strcmp(seg->segname, "__DATA_CONST") != 0) {
        uintptr_t start = (uintptr_t)seg->vmaddr + (uintptr_t)slide;
        fn((void *)start, (void *)(start + seg->vmsize));
      }
    }
    lc = (const struct load_command *)((const char *)lc + lc->cmdsize);
  }
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

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
intptr_t os_syscall6(uintptr_t num, uintptr_t a1, uintptr_t a2, uintptr_t a3, uintptr_t a4,
                     uintptr_t a5, uintptr_t a6) {
  // Darwin has no stable raw syscall ABI; libc syscall() is the supported path.
  extern int syscall(int, ...);
  errno = 0;
  long r = syscall((int)num, a1, a2, a3, a4, a5, a6);
  if (r == -1 && errno != 0) return -(intptr_t)errno;
  return (intptr_t)r;
}
#pragma clang diagnostic pop

#endif

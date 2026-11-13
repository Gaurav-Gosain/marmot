// os_wasi.c: the OS layer on WASI preview 1 (wasip1/wasm), without a libc.
//
// One thread: there is one M, no signals, no sysmon and no other threads.
// Memory is the module's linear memory, which only grows (memory.grow) and
// arrives zeroed: os_map, os_reserve and os_commit grow it, os_unmap and
// os_release keep the pages (the runtime's own free lists reuse them).
// Clocks, sleeping, output and exit go through the wasi_snapshot_preview1
// imports.

#include "internal.h"

#if RT_OS_WASI

#include <stddef.h>

#define WASI(name) __attribute__((import_module("wasi_snapshot_preview1"), import_name(#name)))

typedef struct {
  const void *buf;
  uint32_t len;
} wasi_ciovec;

WASI(fd_write) int32_t wasi_fd_write(int32_t fd, const wasi_ciovec *iovs, uint32_t n, uint32_t *written);
WASI(clock_time_get) int32_t wasi_clock_time_get(uint32_t id, uint64_t precision, uint64_t *time);
WASI(poll_oneoff) int32_t wasi_poll_oneoff(const void *in, void *out, uint32_t n, uint32_t *nevents);
WASI(proc_exit) void wasi_proc_exit(int32_t code) NORETURN;

enum { CLOCK_REALTIME = 0, CLOCK_MONOTONIC = 1 };
// WASI errno values (preview 1), negated in results as elsewhere.
enum { WASI_EBADF = 8, WASI_EINTR = 27, WASI_ENOSYS = 52, WASI_ENOTTY = 59 };

typedef struct {
  void *buf;
  uint32_t len;
} wasi_iovec;
WASI(fd_read) int32_t wasi_fd_read(int32_t fd, const wasi_iovec *iovs, uint32_t n, uint32_t *nread);
WASI(fd_close) int32_t wasi_fd_close(int32_t fd);
typedef struct {
  uint8_t filetype;
  uint8_t pad;
  uint16_t flags;
  uint8_t pad2[4];
  uint64_t rights_base;
  uint64_t rights_inheriting;
} wasi_fdstat;
WASI(fd_fdstat_get) int32_t wasi_fd_fdstat_get(int32_t fd, wasi_fdstat *st);

WASI(args_sizes_get) int32_t wasi_args_sizes_get(uint32_t *argc, uint32_t *size);
WASI(args_get) int32_t wasi_args_get(char **argv, char *buf);
WASI(environ_sizes_get) int32_t wasi_environ_sizes_get(uint32_t *n, uint32_t *size);
WASI(environ_get) int32_t wasi_environ_get(char **env, char *buf);

// The runtime's page: memory is handed out in these (a wasm page is 64 KiB,
// too coarse for stacks and tables).
uintptr_t rt_pagesize = 4096;
rt_m *rt_wasi_m;  // the current M (one thread)

void setm(rt_m *m) { rt_wasi_m = m; }

void os_init(void) {}

// Memory handed out so far ends at brk_cur; linear memory ends at brk_end.
static uintptr_t brk_cur, brk_end;

// n bytes (rounded up to pages) of new zeroed memory, or NULL: what is left
// of linear memory, grown in wasm pages as needed.
static void *grow(uintptr_t n) {
  uintptr_t mem = __builtin_wasm_memory_size(0) << 16;
  if (mem != brk_end) brk_cur = brk_end = mem;  // first call
  if (n > ((uintptr_t)3 << 30)) return NULL;
  n = (n + 4095) & ~(uintptr_t)4095;
  if (n > brk_end - brk_cur) {
    uintptr_t pages = (n - (brk_end - brk_cur) + 65535) >> 16;
    if ((brk_end >> 16) + pages > 65535) return NULL;  // past 4 GiB - 64 KiB
    if (__builtin_wasm_memory_grow(0, pages) == (uintptr_t)-1) return NULL;
    brk_end += pages << 16;
  }
  void *p = (void *)brk_cur;
  brk_cur += n;
  return p;
}

void *os_reserve(uintptr_t n) { return grow(n); }
bool os_commit(void *p, uintptr_t n) { return true; }
void *os_map(uintptr_t n) { return grow(n); }
void os_unmap(void *p, uintptr_t n) {}
void os_release(void *p, uintptr_t n) {}
void os_protect_none(void *p, uintptr_t n) {}

int32_t os_ncpu(void) { return 1; }

int64_t os_nanotime(void) {
  uint64_t t = 0;
  wasi_clock_time_get(CLOCK_MONOTONIC, 1, &t);
  return (int64_t)t;
}

void os_walltime(int64_t *sec, int32_t *nsec) {
  uint64_t t = 0;
  wasi_clock_time_get(CLOCK_REALTIME, 1, &t);
  *sec = (int64_t)(t / 1000000000);
  *nsec = (int32_t)(t % 1000000000);
}

void os_write_err(const void *buf, uintptr_t n) {
  wasi_ciovec iov = {buf, (uint32_t)n};
  uint32_t w;
  wasi_fd_write(2, &iov, 1, &w);
}

void os_exit(int32_t code) { wasi_proc_exit(code); }

// A relative clock subscription (poll_oneoff's layout for wasm32).
typedef struct {
  uint64_t userdata;
  uint8_t tag;  // 0: clock
  uint8_t pad[7];
  uint32_t id;
  uint32_t pad2;
  uint64_t timeout;
  uint64_t precision;
  uint16_t flags;  // 0: relative
  uint8_t pad3[6];
} wasi_subscription;

typedef struct {
  uint64_t userdata;
  uint16_t error;
  uint8_t type;
  uint8_t pad[5];
  uint64_t nbytes;
  uint16_t flags;
  uint8_t pad2[6];
} wasi_event;

static void sleep_ns(int64_t ns) {
  if (ns <= 0) return;
  wasi_subscription s;
  __builtin_memset(&s, 0, sizeof s);
  s.tag = 0;
  s.id = CLOCK_MONOTONIC;
  s.timeout = (uint64_t)ns;
  wasi_event e;
  uint32_t n;
  wasi_poll_oneoff(&s, &e, 1, &n);
}

// With one thread nothing else can change *addr while we wait: a wait
// with a timeout sleeps (the scheduler waits for its next timer this way);
// one without returns at once, as a spurious wakeup.
void os_futex_wait(_Atomic uint32_t *addr, uint32_t val, int64_t ns) {
  if (atomic_load(addr) != val) return;
  if (ns > 0) sleep_ns(ns);
}

void os_futex_wake(_Atomic uint32_t *addr, bool all) {}

void os_usleep(uint32_t us) { sleep_ns((int64_t)us * 1000); }

void os_yield(void) {}

// Data and bss: from the start of the data segment to the end of the data
// (wasm-ld's __global_base and __data_end).
extern char __global_base[] __attribute__((weak));
extern char __data_end[] __attribute__((weak));
void os_data_segments(void (*fn)(void *start, void *end)) {
  if (__data_end) fn(__global_base ? (void *)__global_base : (void *)1024, __data_end);
}

bool os_thread_create(void (*fn)(void *), void *arg, uintptr_t stacksize) { return false; }

intptr_t os_syscall6(uintptr_t num, uintptr_t a1, uintptr_t a2, uintptr_t a3, uintptr_t a4,
                     uintptr_t a5, uintptr_t a6) {
  return -WASI_ENOSYS;  // ENOSYS: no raw system calls on WASI
}

// ---- what signal.c, prof.c and netpoll.c give other targets ----

static void (*sigpanic_fn)(uintptr_t addr, int32_t kind);
RT_API void rt_set_sigpanic(void (*fn)(uintptr_t addr, int32_t kind)) { sigpanic_fn = fn; }
void signal_init(void) {}
void signal_init_thread(rt_m *m) {}
void signal_preempt(rt_m *m) {}
void signal_check_stuck(void) {}
bool signal_waiting(void) { return false; }
bool signal_deliver(void) { return false; }
void signal_raise_sigpipe(void) {}
RT_API void rt_sigpipe(void) {}
RT_API int32_t rt_signal_enable(int32_t sig) { return -WASI_ENOSYS; }
RT_API int32_t rt_signal_disable(int32_t sig) { return -WASI_ENOSYS; }
RT_API int32_t rt_signal_ignore(int32_t sig) { return -WASI_ENOSYS; }
RT_API int32_t rt_signal_ignored(int32_t sig) { return 0; }
// No signal ever arrives: the receiving goroutine waits forever.
RT_API int32_t rt_signal_recv(void) {
  for (;;) rt_park(0, 0);
}

void prof_thread_start(rt_m *m) {}
RT_API int32_t rt_prof_start(int32_t hz) { return -WASI_ENOSYS; }
RT_API void rt_prof_stop(void) {}
RT_API intptr_t rt_prof_read(uint64_t *buf, intptr_t n) { return -1; }

_Atomic int32_t netpoll_waiters;
_Atomic int32_t netpoll_blocked;
_Atomic int64_t netpoll_last;
bool netpoll_inited(void) { return false; }
void netpoll_break(void) {}
int32_t netpoll(int64_t ns) { return 0; }
RT_API void *rt_poll_open(int32_t fd) { return 0; }
RT_API void rt_poll_evict(void *h) {}
RT_API void rt_poll_wake(void *h, int32_t mode) {}
RT_API void rt_poll_close(void *h) {}
RT_API int32_t rt_poll_wait(void *h, int32_t mode, int64_t deadline) { return -WASI_ENOSYS; }

// ---- what io.c and libcall.c give other targets ----

RT_API intptr_t rt_write(int32_t fd, const void *buf, uintptr_t n) {
  wasi_ciovec iov = {buf, (uint32_t)n};
  uint32_t w = 0;
  int32_t e = wasi_fd_write(fd, &iov, 1, &w);
  return e ? -(intptr_t)e : (intptr_t)w;
}

RT_API intptr_t rt_read(int32_t fd, void *buf, uintptr_t n) {
  wasi_iovec iov = {buf, (uint32_t)n};
  uint32_t r = 0;
  int32_t e = wasi_fd_read(fd, &iov, 1, &r);
  return e ? -(intptr_t)e : (intptr_t)r;
}

// A character device (filetype 2) is the closest WASI has to a terminal.
RT_API int32_t rt_isatty(int32_t fd) {
  wasi_fdstat st;
  return wasi_fd_fdstat_get(fd, &st) == 0 && st.filetype == 2;
}

// Files come later (path_open on the preopened directories).
RT_API intptr_t rt_open(const char *path, int32_t flags, uint32_t mode) { return -WASI_ENOSYS; }

RT_API intptr_t rt_close(int32_t fd) {
  int32_t e = wasi_fd_close(fd);
  return e ? -(intptr_t)e : 0;
}

RT_API intptr_t rt_syscall(uintptr_t num, uintptr_t a1, uintptr_t a2, uintptr_t a3, uintptr_t a4,
                           uintptr_t a5, uintptr_t a6) {
  return -WASI_ENOSYS;
}

RT_API int64_t rt_nanotime(void) { return os_nanotime(); }
RT_API void rt_walltime(int64_t *sec, int32_t *nsec) { os_walltime(sec, nsec); }

RT_API intptr_t rt_libcall_raw(void *fn, const uintptr_t *args, int32_t nargs, int32_t nfixed,
                               int32_t *err) {
  *err = WASI_ENOSYS;
  return -1;
}
RT_API intptr_t rt_libcall(void *fn, const uintptr_t *args, int32_t nargs, int32_t nfixed,
                           int32_t *err) {
  *err = WASI_ENOSYS;
  return -1;
}
RT_API intptr_t rt_libcall_f64(void *fn, const uintptr_t *args, double f0, int32_t *err) {
  *err = WASI_ENOSYS;
  return -1;
}

// ---- program entry ----

void rt_wasi_main(int argc, char **argv, char **envp);

static void wasi_main(void *arg) {
  uint32_t argc = 0, asize = 0, envc = 0, esize = 0;
  wasi_args_sizes_get(&argc, &asize);
  wasi_environ_sizes_get(&envc, &esize);
  char **argv = grow((argc + 1 + envc + 1) * sizeof(char *) + asize + esize);
  if (!argv) os_exit(2);
  char **envp = argv + argc + 1;
  char *abuf = (char *)(envp + envc + 1);
  wasi_args_get(argv, abuf);
  wasi_environ_get(envp, abuf + asize);
  argv[argc] = NULL;
  envp[envc] = NULL;
  rt_wasi_main((int)argc, argv, envp);
}

// The module's entry: every context runs on top of rt_ctx_start's loop.
RT_API void _start(void) { rt_ctx_start(wasi_main, NULL); }

// ---- the C library functions the compiler calls (no libc here) ----
// memory.copy and memory.fill (bulk memory) do the work; the loops are
// kept from being turned back into calls of themselves.
#define NOBUILTIN __attribute__((no_builtin))

RT_API void *memcpy(void *restrict d, const void *restrict s, size_t n) {
  __builtin_memcpy(d, s, n);
  return d;
}

RT_API void *memmove(void *d, const void *s, size_t n) {
  __builtin_memmove(d, s, n);
  return d;
}

RT_API void *memset(void *d, int c, size_t n) {
  __builtin_memset(d, c, n);
  return d;
}

NOBUILTIN RT_API int memcmp(const void *a, const void *b, size_t n) {
  const unsigned char *x = a, *y = b;
  for (size_t i = 0; i < n; i++)
    if (x[i] != y[i]) return x[i] < y[i] ? -1 : 1;
  return 0;
}

RT_API int bcmp(const void *a, const void *b, size_t n) { return memcmp(a, b, n); }

// 128-bit multiply (compiler-rt's; there is no builtins library here).
typedef unsigned __int128 u128;
RT_API u128 __multi3(u128 a, u128 b) {
  uint64_t al = (uint64_t)a, ah = (uint64_t)(a >> 64), bl = (uint64_t)b, bh = (uint64_t)(b >> 64);
  uint64_t a0 = al & 0xffffffff, a1 = al >> 32, b0 = bl & 0xffffffff, b1 = bl >> 32;
  uint64_t p00 = a0 * b0, p01 = a0 * b1, p10 = a1 * b0, p11 = a1 * b1;
  uint64_t mid = (p00 >> 32) + (uint32_t)p01 + (uint32_t)p10;
  uint64_t lo = (mid << 32) | (uint32_t)p00;
  uint64_t hi = p11 + (p01 >> 32) + (p10 >> 32) + (mid >> 32) + al * bh + ah * bl;
  return ((u128)hi << 64) | lo;
}

NOBUILTIN RT_API size_t strlen(const char *s) {
  size_t n = 0;
  while (s[n]) n++;
  return n;
}

NOBUILTIN RT_API int strcmp(const char *a, const char *b) {
  while (*a && *a == *b) a++, b++;
  return (unsigned char)*a - (unsigned char)*b;
}

NOBUILTIN RT_API int strncmp(const char *a, const char *b, size_t n) {
  for (; n; n--, a++, b++) {
    if (*a != *b) return (unsigned char)*a - (unsigned char)*b;
    if (!*a) return 0;
  }
  return 0;
}

#endif

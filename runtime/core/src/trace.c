// trace.c: MARMOT_TRACE=wakeups=PATH, a record of every time an M wakes
// from a blocking sleep, for `marmot prof -wakeups`.
//
// PATH gets a 32-byte header ("MRMTWAKE", u32 version 1, u32 pid, u64 the
// image's load address, u64 os_nanotime at start) and then one 48-byte
// record per wakeup, little endian: u64 time (os_nanotime), u8 cause, 3
// bytes of padding, u32 the goroutine made runnable (0: none), u64 pc[3],
// u64 aux. pc are the three frames above the runtime function where the
// cause began (a timer made, a goroutine readied), from the frame pointer
// chain, 0 where it ends. Each record is one write with O_APPEND, so a
// crash keeps the records written so far. Unset, the hooks cost one load
// and a branch.

#include "internal.h"

#if !RT_OS_WASI

#include <fcntl.h>
#include <unistd.h>
#if RT_OS_DARWIN
#include <mach-o/dyld.h>
#endif

int32_t trace_fd = -1;

static bool prefix(const char *s, const char *p, const char **rest) {
  while (*p)
    if (*s++ != *p++) return false;
  *rest = s;
  return true;
}

static void put32(uint8_t *b, uint32_t v) {
  for (int i = 0; i < 4; i++) b[i] = (uint8_t)(v >> (8 * i));
}

static void put64(uint8_t *b, uint64_t v) {
  for (int i = 0; i < 8; i++) b[i] = (uint8_t)(v >> (8 * i));
}

static uint64_t image_base(void) {
#if RT_OS_DARWIN
  return (uint64_t)(uintptr_t)_dyld_get_image_header(0);
#else
  extern char __ehdr_start[] __attribute__((weak));
  return (uint64_t)(uintptr_t)__ehdr_start;
#endif
}

void trace_init(void) {
  const char *v = rt_getenv("MARMOT_TRACE"), *path;
  if (!v || !prefix(v, "wakeups=", &path) || !*path) return;
  int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC | O_APPEND | O_CLOEXEC, 0644);
  if (fd < 0) {
    rt_print("marmot: MARMOT_TRACE: cannot open the trace file\n");
    return;
  }
  uint8_t h[32];
  __builtin_memcpy(h, "MRMTWAKE", 8);
  put32(h + 8, 1);
  put32(h + 12, (uint32_t)getpid());
  put64(h + 16, image_base());
  put64(h + 24, (uint64_t)os_nanotime());
  if (write(fd, h, sizeof h) != (ssize_t)sizeof h) {
    close(fd);
    return;
  }
  trace_fd = fd;
}

// The three frames above the caller of the function that calls this one:
// fp is that function's frame (__builtin_frame_address(0)).
NOINLINE void trace_frames(void *fp, uint64_t pc[3]) {
  uintptr_t f = (uintptr_t)fp;
  for (int i = 0; i < 3; i++) {
    pc[i] = 0;
    if (!f || (f & 7)) continue;
    uintptr_t next = ((uintptr_t *)f)[0];
    pc[i] = ((uintptr_t *)f)[1];
    if (next <= f) {
      f = 0;
      continue;
    }
    f = next;
  }
}

void trace_wake_record(const trace_why *w) {
  uint8_t r[48] = {0};
  put64(r, (uint64_t)os_nanotime());
  r[8] = w->cause;
  put32(r + 12, w->goid);
  for (int i = 0; i < 3; i++) put64(r + 16 + 8 * i, w->pc[i]);
  put64(r + 40, w->aux);
  ssize_t n = write(trace_fd, r, sizeof r);
  (void)n;
}

#endif

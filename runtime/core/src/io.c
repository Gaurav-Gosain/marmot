// io.c: raw I/O and the generic system call escape hatch. Every call that can
// block is bracketed by rt_entersyscall/rt_exitsyscall so the scheduler can
// hand the P to another thread.

#include "internal.h"

#include <errno.h>
#include <fcntl.h>
#include <unistd.h>

RT_API intptr_t rt_write(int32_t fd, const void *buf, uintptr_t n) {
  rt_entersyscall();
  intptr_t r;
  do {
    r = write(fd, buf, n);
  } while (r < 0 && errno == EINTR);
  if (r < 0) r = -(intptr_t)errno;
  rt_exitsyscall();
  if (r == -EPIPE && (fd == 1 || fd == 2)) signal_raise_sigpipe();
  return r;
}

RT_API int32_t rt_isatty(int32_t fd) { return isatty(fd) == 1; }

RT_API intptr_t rt_read(int32_t fd, void *buf, uintptr_t n) {
  rt_entersyscall();
  intptr_t r;
  do {
    r = read(fd, buf, n);
  } while (r < 0 && errno == EINTR);
  if (r < 0) r = -(intptr_t)errno;
  rt_exitsyscall();
  return r;
}

RT_API intptr_t rt_open(const char *path, int32_t flags, uint32_t mode) {
  rt_entersyscall();
  intptr_t r;
  do {
    r = open(path, flags | O_CLOEXEC, (int)mode);
  } while (r < 0 && errno == EINTR);
  if (r < 0) r = -(intptr_t)errno;
  rt_exitsyscall();
  return r;
}

RT_API intptr_t rt_close(int32_t fd) {
  intptr_t r = close(fd);
  if (r < 0) r = -(intptr_t)errno;
  return r;
}

RT_API intptr_t rt_syscall(uintptr_t num, uintptr_t a1, uintptr_t a2, uintptr_t a3,
                           uintptr_t a4, uintptr_t a5, uintptr_t a6) {
  rt_entersyscall();
  intptr_t r = os_syscall6(num, a1, a2, a3, a4, a5, a6);
  rt_exitsyscall();
  return r;
}

// ---- time ----

RT_API int64_t rt_nanotime(void) { return os_nanotime(); }

RT_API void rt_walltime(int64_t *sec, int32_t *nsec) { os_walltime(sec, nsec); }

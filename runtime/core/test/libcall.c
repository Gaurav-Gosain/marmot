// rt_libcall: fixed and variadic calls, errno reporting.
// expect-stdout: ok
#include "rtest.h"
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

static long sum9(long a, long b, long c, long d, long e, long f, long g, long h, long i) {
  return a + 2 * b + 3 * c + 4 * d + 5 * e + 6 * f + 7 * g + 8 * h + 9 * i;
}

void marmot_main(void) {
  int32_t err = -1;
  uintptr_t a9[9] = {1, 2, 3, 4, 5, 6, 7, 8, 9};
  CHECK(rt_libcall((void *)sum9, a9, 9, 9, &err) == 285);
  char path[] = "/dev/null";
  uintptr_t oa[3] = {(uintptr_t)path, O_WRONLY | O_CREAT, 0644};
  intptr_t fd = rt_libcall((void *)open, oa, 3, 2, &err);
  CHECK(fd >= 0);
  uintptr_t fa[3] = {(uintptr_t)fd, F_GETFD, 0};
  CHECK(rt_libcall_raw((void *)fcntl, fa, 3, 2, &err) >= 0);
  char bad[] = "/nonexistent/dir/x";
  uintptr_t ob[3] = {(uintptr_t)bad, O_RDONLY, 0};
  CHECK((int32_t)rt_libcall((void *)open, ob, 3, 2, &err) == -1 && err == ENOENT);
  uintptr_t ca[1] = {(uintptr_t)fd};
  CHECK(rt_libcall((void *)close, ca, 1, 1, &err) == 0);
  uintptr_t pa[0];
  CHECK(rt_libcall((void *)getpid, pa, 0, 0, &err) > 0);
  out("ok\n");
}

// Process entry, args, env, write, time, setjmp/longjmp, alloc, locks.
// expect-stdout: hello from marmot_main
#include "rt.h"
#include <string.h>

#define CHECK(c)                                                    \
  do {                                                              \
    if (!(c)) {                                                     \
      rt_fatal("check failed: " #c, sizeof("check failed: " #c) - 1); \
    }                                                               \
  } while (0)

static int depth;
static void jumper(void *buf, int n) {
  depth++;
  if (n == 0) rt_longjmp(buf, 7);
  jumper(buf, n - 1);
}

void marmot_main(void) {
  const char msg[] = "hello from marmot_main\n";
  CHECK(rt_write(1, msg, sizeof msg - 1) == (intptr_t)(sizeof msg - 1));
  CHECK(rt_argc() >= 1 && rt_argv()[0] != 0);
  CHECK(rt_envp() != 0);
  CHECK(rt_write(-1, msg, 1) < 0);

  int64_t t0 = rt_nanotime();
  int64_t sec;
  int32_t nsec;
  rt_walltime(&sec, &nsec);
  CHECK(sec > 1600000000 && nsec >= 0 && nsec < 1000000000);
  rt_sleep(2000000);
  CHECK(rt_nanotime() - t0 >= 2000000);

  void *buf[RT_JMPBUF_WORDS];
  volatile int count = 0;
  int r = rt_setjmp(buf);
  count++;
  if (r == 0) jumper(buf, 50);
  CHECK(r == 7 && count == 2 && depth == 51);

  r = rt_setjmp(buf);
  if (r == 0) rt_longjmp(buf, 0);
  CHECK(r == 1);

  for (int i = 1; i < 5000; i += 7) {
    unsigned char *p = rt_alloc((uintptr_t)i, RT_ALLOC_NOSCAN, 0, 0);
    for (int j = 0; j < i; j++) CHECK(p[j] == 0);
    memset(p, 0xab, (size_t)i);
  }
  CHECK(rt_alloc(0, RT_ALLOC_NOSCAN, 0, 0) != 0);

  rt_mutex mu = {0};
  rt_lock(&mu);
  rt_unlock(&mu);

#if !defined(__wasm__)  // no /dev/null under WASI
  char path[] = "/dev/null";
  intptr_t fd = rt_open(path, 1 /* O_WRONLY */, 0);
  CHECK(fd >= 0);
  CHECK(rt_write((int32_t)fd, msg, 3) == 3);
  CHECK(rt_close((int32_t)fd) == 0);
#endif
  CHECK(rt_open("/nonexistent/x", 0, 0) < 0);
}

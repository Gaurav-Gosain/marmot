// Stop the world reaches goroutines in tight loops (signal preemption) and in
// syscalls; GOMAXPROCS can grow and shrink at run time.
// expect-stdout: ok
// tsan: skip (ThreadSanitizer defers async signals until a syscall, so tight loops never stop)
// env: GOMAXPROCS=4
// timeout: 60
#include "rtest.h"
#include <unistd.h>

static volatile int stop;
static volatile long spins[8];
static waitgroup wg;

static void spinner(void *arg) {
  intptr_t i = (intptr_t)arg;
  while (!stop) spins[i]++;
  wg_done(&wg);
}

static void sleeper(void *arg) {
  // Blocks in a real syscall.
  struct { int fds[2]; } *p = arg;
  char c;
  rt_read(p->fds[0], &c, 1);
  wg_done(&wg);
}

void marmot_main(void) {
  struct { int fds[2]; } pp;
  CHECK(pipe(pp.fds) == 0);
  wg_add(&wg, 4);
  for (intptr_t i = 0; i < 3; i++) rt_go(spinner, (void *)i);
  rt_go(sleeper, &pp);
  rt_sleep(20000000);
  for (int i = 0; i < 20; i++) {
    CHECK(rt_gomaxprocs(i % 2 ? 4 : 6) == (i == 0 ? 4 : (i % 2 ? 6 : 4)));
  }
  CHECK(rt_gomaxprocs(2) == 4);
  CHECK(rt_gomaxprocs(8) == 2);
  rt_sleep(20000000);
  stop = 1;
  CHECK(rt_write(pp.fds[1], "x", 1) == 1);
  wg_wait(&wg);
  CHECK(spins[0] > 0 && spins[1] > 0 && spins[2] > 0);
  out("ok\n");
}

// A goroutine blocked in a read must not stop others (GOMAXPROCS=1), and
// timers fire while main is blocked in a syscall.
// expect-stdout: ok
// env: GOMAXPROCS=1
#include "rtest.h"
#include <unistd.h>

static int fds[2];
static volatile int progress;

static void writer(void *arg) {
  for (int i = 0; i < 50; i++) {
    progress++;
    rt_sleep(100000);
  }
  CHECK(rt_write(fds[1], "x", 1) == 1);
}

static int fired;
static void on_timer(void *arg) { fired = 1; }

void marmot_main(void) {
  CHECK(pipe(fds) == 0);
  rt_go(writer, 0);
  rt_timer_start(rt_nanotime() + 1000000, on_timer, 0);
  char c;
  CHECK(rt_read(fds[0], &c, 1) == 1);
  CHECK(c == 'x' && progress == 50 && fired == 1);
  out("ok\n");
}

// os/signal support: enable, recv, ignore, no deadlock report while waiting.
// expect-stdout: ok
// env: GOMAXPROCS=2
#include "rtest.h"
#include <pthread.h>
#include <signal.h>
#include <unistd.h>

static waitgroup wg;
static int got[3];

static void receiver(void *arg) {
  for (int i = 0; i < 3; i++) got[i] = rt_signal_recv();
  wg_done(&wg);
}

static void *later(void *arg) {
  usleep(50000);
  kill(getpid(), SIGUSR2);
  return 0;
}

void marmot_main(void) {
  CHECK(rt_signal_enable(SIGUSR1) == 0);
  CHECK(rt_signal_enable(SIGUSR2) == 0);
  CHECK(rt_signal_enable(SIGHUP) == 0);
  CHECK(rt_signal_enable(SIGKILL) < 0);
  wg_add(&wg, 1);
  rt_go(receiver, 0);
  rt_sleep(5000000);
  kill(getpid(), SIGUSR1);
  rt_sleep(5000000);
  kill(getpid(), SIGHUP);
  // Everyone blocked (main in wg_wait, receiver in rt_signal_recv): not a
  // deadlock, a signal from another thread ends it.
  pthread_t t;
  pthread_create(&t, 0, later, 0);
  wg_wait(&wg);
  CHECK(got[0] == SIGUSR1 && got[1] == SIGHUP && got[2] == SIGUSR2);
  CHECK(rt_signal_ignored(SIGUSR1) == 0);
  CHECK(rt_signal_ignore(SIGUSR1) == 0);
  CHECK(rt_signal_ignored(SIGUSR1) == 1);
  kill(getpid(), SIGUSR1);  // ignored: no effect
  CHECK(rt_signal_disable(SIGHUP) == 0);
  rt_sleep(1000000);
  out("ok\n");
}

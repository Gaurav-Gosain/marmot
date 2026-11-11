// Poller: readers park without holding threads, deadlines, eviction.
// expect-stdout: ok
// env: GOMAXPROCS=1
// timeout: 60
#include "rtest.h"
#include <errno.h>
#include <fcntl.h>
#include <unistd.h>

#define NP 100
static int fds[NP][2];
static void *pds[NP];
static waitgroup wg;
static long total;

static void nonblock(int fd) { fcntl(fd, F_SETFL, fcntl(fd, F_GETFL) | O_NONBLOCK); }

static long read_full(int i, char *buf, long n) {
  long got = 0;
  while (got < n) {
    intptr_t r = rt_read(fds[i][0], buf + got, (uintptr_t)(n - got));
    if (r > 0) {
      got += r;
      continue;
    }
    if (r == -EAGAIN) {
      CHECK(rt_poll_wait(pds[i], 'r', 0) == 0);
      continue;
    }
    rt_fatal("read failed", 11);
  }
  return got;
}

static void reader(void *arg) {
  int i = (int)(intptr_t)arg;
  char buf[64];
  for (int k = 0; k < 20; k++) {
    read_full(i, buf, 8);
    __atomic_fetch_add(&total, buf[0], __ATOMIC_RELAXED);
  }
  wg_done(&wg);
}

static volatile long ticks;
static void ticker(void *arg) {
  while (ticks < 1000000000) {
    ticks++;
    if (ticks % 1000 == 0) rt_gosched();
    if (wg.n == 0) break;
  }
}

static void evicted(void *arg) {
  CHECK(rt_poll_wait(arg, 'r', 0) == 2);
  wg_done(&wg);
}

void marmot_main(void) {
  for (int i = 0; i < NP; i++) {
    CHECK(pipe(fds[i]) == 0);
    nonblock(fds[i][0]);
    pds[i] = rt_poll_open(fds[i][0]);
    CHECK(pds[i] != 0);
  }
  wg_add(&wg, NP);
  for (int i = 0; i < NP; i++) rt_go(reader, (void *)(intptr_t)i);
  rt_go(ticker, 0);
  // Writers: main writes in rounds with sleeps; readers must make progress
  // while parked, and the ticker keeps running.
  char msg[8] = {1, 0, 0, 0, 0, 0, 0, 0};
  for (int k = 0; k < 20; k++) {
    for (int i = 0; i < NP; i++) CHECK(write(fds[i][1], msg, 8) == 8);
    rt_sleep(1000000);
  }
  wg_wait(&wg);
  CHECK(total == NP * 20);
  CHECK(ticks > 0);

  // Deadline.
  int64_t t0 = rt_nanotime();
  CHECK(rt_poll_wait(pds[0], 'r', rt_nanotime() + 20000000) == 1);
  CHECK(rt_nanotime() - t0 >= 20000000);
  CHECK(rt_poll_wait(pds[0], 'r', rt_nanotime() - 1) == 1);
  // Writable right away (edge seen at registration).
  // Eviction wakes a parked reader with 2.
  wg_add(&wg, 1);
  rt_go(evicted, pds[1]);
  rt_sleep(2000000);
  rt_poll_evict(pds[1]);
  wg_wait(&wg);
  for (int i = 0; i < NP; i++) {
    rt_poll_close(pds[i]);
    close(fds[i][0]);
    close(fds[i][1]);
  }
  out("ok\n");
}

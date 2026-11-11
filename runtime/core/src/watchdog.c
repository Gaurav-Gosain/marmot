// watchdog.c: MARMOT_DEADLOCK_DEBUG. A plain thread (not an M) samples the
// scheduler once a second; after MARMOT_DEADLOCK_SECS (default 10) seconds
// without any goroutine switch, syscall, or stop/start of the world it
// prints every M, P and G (sched_dump) to stderr, once per stall. With
// MARMOT_DEADLOCK_DEBUG=2 it then exits with status 2.
//
// A program that legitimately does nothing for that long (a goroutine
// blocked in one long read, a sleep) also produces a dump: this is a debug
// aid, not a detector.

#include "internal.h"

uint64_t sched_progress(void);

static int64_t wd_secs = 10;
static bool wd_exit;

static void watchdog(void *arg) {
  (void)arg;
  uint64_t last = sched_progress();
  int64_t still = 0;
  bool dumped = false;
  for (;;) {
    os_usleep(1000000);
    uint64_t now = sched_progress();
    if (now != last) {
      last = now;
      still = 0;
      dumped = false;
      continue;
    }
    if (++still < wd_secs || dumped) continue;
    dumped = true;
    rt_print("marmot: MARMOT_DEADLOCK_DEBUG: no scheduling progress for ");
    rt_printi(still);
    rt_print(" s\n");
    sched_dump();
    if (wd_exit) os_exit(2);
  }
}

void watchdog_start(void) {
  bool ok;
  int64_t s = rt_atoi(rt_getenv("MARMOT_DEADLOCK_SECS"), &ok);
  if (ok && s > 0) wd_secs = s;
  const char *e = rt_getenv("MARMOT_DEADLOCK_DEBUG");
  wd_exit = e && e[0] == '2';
  if (!os_thread_create(watchdog, NULL, 64 << 10)) rt_print("marmot: cannot start the deadlock watchdog\n");
}

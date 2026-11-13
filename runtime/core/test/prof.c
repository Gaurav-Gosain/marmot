// rt_prof_start samples a busy loop; records carry the pc and the frame
// pointer chain; rt_prof_read returns -1 once stopped and drained.
// expect-stdout: ok
#include "rtest.h"
#include <stdio.h>
#include <time.h>
#ifndef __APPLE__
#define __APPLE__ 0
#endif

static volatile uint64_t sink;

static int64_t cpu_ns(void) {
  struct timespec ts;
  clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &ts);
  return (int64_t)ts.tv_sec * 1000000000 + ts.tv_nsec;
}

// busy runs until the process has used ns of CPU time (wall time varies
// with load; samples follow CPU time).
__attribute__((noinline)) static void busy(int64_t ns) {
  int64_t end = cpu_ns() + ns;
  while (cpu_ns() < end)
    for (int i = 0; i < 1000; i++) sink += (uint64_t)i * (uint64_t)i;
}

void marmot_main(void) {
  if (rt_prof_start(1000) != 0) {
    printf("start failed\n");
    fflush(stdout); rt_exit(1);
  }
  busy(300000000);
  rt_prof_stop();
  static uint64_t buf[1 << 16];
  int records = 0, deep = 0;
  for (;;) {
    intptr_t k = rt_prof_read(buf, sizeof buf / sizeof buf[0]);
    if (k < 0) break;
    if (k == 0) continue;
    for (intptr_t i = 0; i < k;) {
      uint64_t len = buf[i];
      if (len < 4 || i + (intptr_t)len > k) {
        printf("bad record len %llu\n", (unsigned long long)len);
        fflush(stdout); rt_exit(1);
      }
      if (buf[i + 2] == 1) {
        records++;
        if (buf[i + 3] == 0) {
          printf("zero pc\n");
          fflush(stdout); rt_exit(1);
        }
        if (len >= 5) deep++;
      }
      i += (intptr_t)len;
    }
  }
  if (records < 50) {
    printf("only %d samples\n", records);
    fflush(stdout); rt_exit(1);
  }
  // C code on linux is compiled without frame pointers at -O1, so only
  // darwin (which always keeps them) can check the walk here; Go code
  // compiled by marmot keeps frame pointers on every target.
  if (__APPLE__ && deep * 2 < records) {
    printf("stacks too short: %d of %d have callers\n", deep, records);
    fflush(stdout); rt_exit(1);
  }
  printf("ok\n");
  fflush(stdout);
  rt_exit(0);
}

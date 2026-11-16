#include <stdint.h>
#include <stdio.h>
double a(int64_t, int64_t); double sum(int64_t); int64_t twice(void *, int64_t, int64_t); int64_t usekeep(int64_t);
int main(void) {
  int bad = 0;
  double want = 0;
  for (int64_t i = 0; i < 1000; i++) want += 1.0 / (double)((i + 1000) * (i + 1000 + 1) / 2 + i + 1);
  double got = sum(1000);
  if (got != want) { printf("sum: %.17g want %.17g\n", got, want); bad = 1; }
  if (twice(0, 3, 4) != 43) { printf("twice: %lld\n", (long long)twice(0, 3, 4)); bad = 1; }
  if (usekeep(41) != 42) { printf("usekeep: %lld\n", (long long)usekeep(41)); bad = 1; }
  return bad;
}

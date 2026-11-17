#include <stdint.h>
#include <stdio.h>
double loop(int64_t, int64_t);
double ext(int64_t x) {
  // Clobber every caller saved XMM and GPR the C compiler is free to use.
  volatile double d[16];
  for (int i = 0; i < 16; i++) d[i] = (double)(x + i) * 1.5;
  return (double)(x % 7) + d[3] * 0.0;
}
int main(void) {
  double acc = 0;
  for (int64_t i = 0; i < 100; i++) acc = (acc * 0.5 + acc * 0.25 + acc + ext(i * 3)) * 0.5;
  double got = loop(100, 3);
  if (got != acc) { printf("loop: %.17g want %.17g\n", got, acc); return 1; }
  return 0;
}

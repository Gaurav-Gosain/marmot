#include <stdint.h>
#include <stdio.h>
int64_t take8(int8_t c) { return c; }
int64_t take16(int16_t d) { return d; }
int64_t takeu8(uint8_t c) { return c; }
int64_t call8(int8_t a);
int64_t call16(void);
int64_t callu8(void);
int8_t neg8(int8_t a);

int main(void) {
  volatile int8_t m1 = -1;
  int64_t a = call8(m1), b = call16(), c = callu8(), d = neg8(0);
  if (a != -1 || b != -2 || c != 255 || d != -1) {
    printf("ext: %lld %lld %lld %lld\n", (long long)a, (long long)b, (long long)c, (long long)d);
    return 1;
  }
  return 0;
}

// Driver for machotest: links against the object machotest writes.
#include <stdint.h>
#include <stdio.h>

int64_t t0_add(int64_t, int64_t);
int64_t t0_call(int64_t);
int64_t t0_second(void);
int64_t t0_ext(void);
extern int64_t t0_data[2];
extern int64_t *t0_ptr;
extern int64_t t0_bss[8];

int64_t ext_var = 777;
int64_t ext_twice(int64_t x) { return 2 * x; }

int main(void) {
  int bad = 0;
  if (t0_add(40, 2) != 42) bad |= 1;
  if (t0_call(20) != 41) bad |= 2;
  if (t0_second() != 4242) bad |= 4;
  if (t0_ext() != 777) bad |= 8;
  if (t0_ptr != &t0_data[1] || *t0_ptr != 4242) bad |= 16;
  if (t0_bss[3] != 0) bad |= 32;
  t0_bss[3] = 5;
  printf(bad ? "FAIL %d\n" : "ok\n", bad);
  return bad != 0;
}

#include <stdio.h>
#include <stdint.h>
long store_card(int64_t *slot, int64_t v, _Bool on, int64_t a, int64_t b);
static void *last; static int calls;
#if defined(__aarch64__)
void rt_card_x0(void *p) { last = p; calls++; }
#else
void rt_card_rdi(void *p) { last = p; calls++; }
#endif
int main(void) {
  int64_t x = 0, y = 0;
  if (store_card(&x, 5, 0, 1, 2) != 8 || x != 5 || calls != 0) { puts("off: bad"); return 1; }
  if (store_card(&y, 7, 1, 10, 20) != 37 || y != 7 || calls != 1 || last != &y) { puts("on: bad"); return 1; }
  return 0;
}

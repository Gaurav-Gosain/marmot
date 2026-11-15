#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
int64_t dsum(int64_t *p, int64_t n);
int64_t join(int64_t a, int64_t b);
int64_t gsum(int64_t *p, int64_t n, int64_t len);
int64_t gsum2(int64_t *p, int64_t i0, int64_t len);
void panic_hit(int64_t k) { exit(10 + (int)k); }
int main(int argc, char **argv) {
  if (argc > 1 && strcmp(argv[1], "nil") == 0) dsum(0, 3);  /* exits 11 */
  if (argc > 1 && strcmp(argv[1], "neg") == 0) gsum2(0, -1, 2);  /* exits 14 */
  int64_t a[5] = {1, 2, 3, 4, 5};
  if (dsum(a, 5) != 15 || dsum(a, 0) != 0) { puts("dsum: bad"); return 1; }
  if (gsum(a, 5, 5) != 15 || gsum2(a, 2, 5) != 12) { puts("gsum: bad"); return 1; }
  if (join(1, 2) != 1 || join(2, 1) != 2) { puts("join: bad"); return 1; }
  return 0;
}

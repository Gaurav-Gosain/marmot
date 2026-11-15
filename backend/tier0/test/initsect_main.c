#include <stdint.h>
#include <stdio.h>
void pkg_init(void) __asm__(
#ifdef __APPLE__
    "_pkg.init"
#else
    "pkg.init"
#endif
);
int64_t get(void);
int64_t twice(int64_t);
int main(void) {
  pkg_init();  /* counter = (0 + 1) * 2 */
  pkg_init();  /* counter = (2 + 1) * 2 */
  if (get() != 7 || twice(21) != 42) { puts("initsect: bad"); return 1; }
  return 0;
}

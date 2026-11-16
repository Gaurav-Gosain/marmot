#include <stdint.h>
#include <stdio.h>
double s64both(int64_t, float *); double u32both(uint32_t, float *);
int main(void) {
  int bad = 0;
  float f = 0;
  double d = s64both((1LL << 53) + 1, &f);
  if (f != (float)((1LL << 53) + 1) || d != (double)((1LL << 53) + 1)) { printf("s64both: %.9g %.17g\n", f, d); bad = 1; }
  d = u32both(4294967295u, &f);
  if (f != (float)4294967295u || d != 4294967295.0) { printf("u32both: %.9g %.17g\n", f, d); bad = 1; }
  return bad;
}

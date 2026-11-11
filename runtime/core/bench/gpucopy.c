#include "rt.h"
#include <stdio.h>
#include <string.h>
void marmot_main(void) {
  uintptr_t sizes[] = {1 << 20, 16 << 20, 256 << 20};
  for (int s = 0; s < 3; s++) {
    uintptr_t n = sizes[s];
    char *h = rt_alloc(n, 0, 0, 0); memset(h, 1, n);
    void *d = rt_gpu_alloc(n);
    rt_gpu_copy_in(d, h, n); rt_gpu_copy_out(h, d, n);
    int64_t t0 = rt_nanotime(); for (int i = 0; i < 4; i++) rt_gpu_copy_in(d, h, n);
    int64_t t1 = rt_nanotime(); for (int i = 0; i < 4; i++) rt_gpu_copy_out(h, d, n);
    int64_t t2 = rt_nanotime();
    printf("%4lu MiB: h2d %.2f GB/s d2h %.2f GB/s\n", n >> 20, 4.0 * n / (t1 - t0), 4.0 * n / (t2 - t1));
    rt_gpu_free(d, n);
  }
  fflush(stdout);
}

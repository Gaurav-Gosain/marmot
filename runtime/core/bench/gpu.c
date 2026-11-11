// GPU offload against rt_parallel_for and serial code, same kernels as
// test/gpu.c. Run on a machine with CUDA (linux-host).
#include "rt.h"
#include <stdio.h>
#include <string.h>

#pragma clang fp contract(off)

static const char ptx[] = {
#embed "../gpu/kernels_test.ptx"
    , 0};

static double now_ms(void) { return rt_nanotime() / 1e6; }
static uint64_t u64f(double d) { uint64_t u; memcpy(&u, &d, 8); return u; }

typedef struct { uint64_t w, maxit; double sx, ox, sy, oy; uint32_t *out; } mctx;
static void mandel_cpu(void *c, int64_t lo, int64_t hi) {
  mctx *m = c;
  for (int64_t i = lo; i < hi; i++) {
    double cr = (double)((uint64_t)i % m->w) * m->sx + m->ox, ci = (double)((uint64_t)i / m->w) * m->sy + m->oy, zr = 0, zi = 0;
    uint64_t k = 0;
    for (; k < m->maxit; k++) {
      double zr2 = zr * zr, zi2 = zi * zi;
      if (zr2 + zi2 > 4.0) break;
      double t = zr * zi;
      t = t + t;
      zi = t + ci;
      zr = (zr2 - zi2) + cr;
    }
    m->out[i] = (uint32_t)k;
  }
}

typedef struct { uint64_t m; double *a, *b, *c; } mmctx;
static void mm_cpu(void *c, int64_t lo, int64_t hi) {
  mmctx *x = c;
  for (int64_t i = lo; i < hi; i++) {
    uint64_t r = (uint64_t)i / x->m, col = (uint64_t)i % x->m;
    double s = 0;
    for (uint64_t k = 0; k < x->m; k++) s = s + x->a[r * x->m + k] * x->b[k * x->m + col];
    x->c[i] = s;
  }
}

typedef struct { double a, *x, *y; } sctx;
static void saxpy_cpu(void *c, int64_t lo, int64_t hi) {
  sctx *s = c;
  for (int64_t i = lo; i < hi; i++) s->y[i] = s->a * s->x[i] + s->y[i];
}

static void row(const char *name, double serial, double par, double gpu_all, double gpu_kernel, int same, int64_t be) {
  printf("%-28s serial %8.1f ms  parallel_for %7.1f ms  gpu+copies %7.1f ms  gpu kernel %7.2f ms  identical %s  break-even n %lld\n",
         name, serial, par, gpu_all, gpu_kernel, same ? "yes" : "NO", (long long)be);
  fflush(stdout);
}

void marmot_main(void) {
  rt_gpu_info info;
  if (!rt_gpu_info_get(&info)) { printf("no GPU\n"); return; }
  printf("GPU: %d SMs cc %d.%d %d MHz, launch %lld ns, h2d %.1f GB/s, d2h %.1f GB/s, CPU procs %d\n", info.sm_count,
         info.cc_major, info.cc_minor, info.clock_mhz, (long long)info.launch_ns, info.h2d_gbps, info.d2h_gbps, rt_gomaxprocs(0));
  void *mod = rt_gpu_module(ptx, sizeof ptx);
  void *fm = rt_gpu_function(mod, "mandel"), *fmm = rt_gpu_function(mod, "matmul"), *fs = rt_gpu_function(mod, "saxpy");

  // Mandelbrot 2048x2048, 1000 iterations.
  {
    uint64_t w = 2048, h = 2048, n = w * h;
    mctx c = {w, 1000, 3.0 / (double)w, -2.0, 2.4 / (double)h, -1.2, rt_alloc(n * 4, 0, 0, 0)};
    uint32_t *g = rt_alloc(n * 4, 0, 0, 0);
    double t0 = now_ms(); mandel_cpu(&c, 0, (int64_t)n); double t1 = now_ms();
    rt_parallel_for((int64_t)n, 4096, mandel_cpu, &c); double t2 = now_ms();
    void *d = rt_gpu_alloc(n * 4);
    uint64_t args[7] = {w, 1000, u64f(c.sx), u64f(c.ox), u64f(c.sy), u64f(c.oy), (uint64_t)(uintptr_t)d};
    rt_gpu_launch(fm, (int64_t)n, 128, args, 7); rt_gpu_sync();  // warm
    double t3 = now_ms(); rt_gpu_launch(fm, (int64_t)n, 128, args, 7); rt_gpu_sync(); double t4 = now_ms();
    rt_gpu_copy_out(g, d, n * 4); double t5 = now_ms();
    row("mandelbrot 2048^2 x1000", t1 - t0, t2 - t1, t5 - t3, t4 - t3, memcmp(g, c.out, n * 4) == 0,
        rt_gpu_break_even(10.0 * 300, 0, 4));
    rt_gpu_free(d, n * 4);
  }
  // Matmul 1024, float64, naive k order.
  {
    uint64_t m = 1024, n = m * m;
    mmctx x = {m, rt_alloc(n * 8, 0, 0, 0), rt_alloc(n * 8, 0, 0, 0), rt_alloc(n * 8, 0, 0, 0)};
    for (uint64_t i = 0; i < n; i++) { x.a[i] = (double)(i % 17) * 0.1 + 0.3; x.b[i] = 1.0 / (double)(i % 23 + 1); }
    double *g = rt_alloc(n * 8, 0, 0, 0);
    double t0 = now_ms(); mm_cpu(&x, 0, (int64_t)(n / 16)); double t1 = now_ms();  // 1/16, scaled below
    rt_parallel_for((int64_t)n, 1024, mm_cpu, &x); double t2 = now_ms();
    void *dA = rt_gpu_alloc(n * 8), *dB = rt_gpu_alloc(n * 8), *dC = rt_gpu_alloc(n * 8);
    double t3 = now_ms();
    rt_gpu_copy_in(dA, x.a, n * 8); rt_gpu_copy_in(dB, x.b, n * 8);
    uint64_t args[4] = {m, (uint64_t)(uintptr_t)dA, (uint64_t)(uintptr_t)dB, (uint64_t)(uintptr_t)dC};
    double t4 = now_ms(); rt_gpu_launch(fmm, (int64_t)n, 128, args, 4); rt_gpu_sync(); double t5 = now_ms();
    rt_gpu_copy_out(g, dC, n * 8); double t6 = now_ms();
    row("matmul 1024 f64 naive", (t1 - t0) * 16, t2 - t1, t6 - t3, t5 - t4, memcmp(g, x.c, n * 8) == 0,
        rt_gpu_break_even(2.0 * 1024, 16, 8));
  }
  // saxpy 32M: memory bound, transfers dominate.
  {
    int64_t n = 32 << 20;
    sctx s = {1.5, rt_alloc((uintptr_t)n * 8, 0, 0, 0), rt_alloc((uintptr_t)n * 8, 0, 0, 0)};
    for (int64_t i = 0; i < n; i++) { s.x[i] = (double)i; s.y[i] = 1.0; }
    double *gy = rt_alloc((uintptr_t)n * 8, 0, 0, 0);
    memcpy(gy, s.y, (size_t)n * 8);
    double t0 = now_ms(); saxpy_cpu(&s, 0, n); double t1 = now_ms();
    for (int64_t i = 0; i < n; i++) s.y[i] = 1.0;
    double t1b = now_ms(); rt_parallel_for(n, 0, saxpy_cpu, &s); double t2 = now_ms();
    void *dx = rt_gpu_alloc((uintptr_t)n * 8), *dy = rt_gpu_alloc((uintptr_t)n * 8);
    double t3 = now_ms();
    rt_gpu_copy_in(dx, s.x, (uintptr_t)n * 8); rt_gpu_copy_in(dy, gy, (uintptr_t)n * 8);
    uint64_t args[3] = {u64f(1.5), (uint64_t)(uintptr_t)dx, (uint64_t)(uintptr_t)dy};
    double t4 = now_ms(); rt_gpu_launch(fs, n, 256, args, 3); rt_gpu_sync(); double t5 = now_ms();
    rt_gpu_copy_out(gy, dy, (uintptr_t)n * 8); double t6 = now_ms();
    row("saxpy 32M f64", t1 - t0, t2 - t1b, t6 - t3, t5 - t4, memcmp(gy, s.y, (size_t)n * 8) == 0,
        rt_gpu_break_even(2, 16, 8));
  }
}

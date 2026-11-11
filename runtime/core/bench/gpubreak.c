// Break-even problem sizes for GPU offload against rt_parallel_for, with the
// same hand-written kernels as test/gpu.c. GPU time includes copies in and
// out and the sync (warm module, warm buffer pool); the one-time start cost
// (driver, context, JIT) is printed separately. Run with GOMAXPROCS=P on a
// CUDA machine; "par/2" is an ideal-scaling estimate for 2P threads.
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

static void *fm, *fmm;

static void mandel(uint64_t w, uint64_t maxit, double *par, double *gpu, int *same) {
  uint64_t n = w * w;
  mctx c = {w, maxit, 3.0 / (double)w, -2.0, 2.4 / (double)w, -1.2, rt_alloc(n * 4, 0, 0, 0)};
  uint32_t *g = rt_alloc(n * 4, 0, 0, 0);
  *par = *gpu = 1e30;
  for (int r = 0; r < 3; r++) {
    double t0 = now_ms();
    rt_parallel_for((int64_t)n, n / 1024 > 64 ? (int64_t)(n / 1024) : 64, mandel_cpu, &c);
    double t1 = now_ms();
    if (t1 - t0 < *par) *par = t1 - t0;
    t0 = now_ms();
    void *d = rt_gpu_alloc(n * 4);
    uint64_t args[7] = {w, maxit, u64f(c.sx), u64f(c.ox), u64f(c.sy), u64f(c.oy), (uint64_t)(uintptr_t)d};
    rt_gpu_launch(fm, (int64_t)n, 128, args, 7);
    rt_gpu_copy_out(g, d, n * 4);
    rt_gpu_free(d, n * 4);
    t1 = now_ms();
    if (t1 - t0 < *gpu) *gpu = t1 - t0;
  }
  *same = memcmp(g, c.out, n * 4) == 0;
}

static void matmul(uint64_t m, double *par, double *gpu, int *same) {
  uint64_t n = m * m;
  mmctx x = {m, rt_alloc(n * 8, 0, 0, 0), rt_alloc(n * 8, 0, 0, 0), rt_alloc(n * 8, 0, 0, 0)};
  for (uint64_t i = 0; i < n; i++) { x.a[i] = (double)(i % 17) * 0.1 + 0.3; x.b[i] = 1.0 / (double)(i % 23 + 1); }
  double *g = rt_alloc(n * 8, 0, 0, 0);
  *par = *gpu = 1e30;
  for (int r = 0; r < 3; r++) {
    double t0 = now_ms();
    rt_parallel_for((int64_t)n, 0, mm_cpu, &x);
    double t1 = now_ms();
    if (t1 - t0 < *par) *par = t1 - t0;
    t0 = now_ms();
    void *dA = rt_gpu_alloc(n * 8), *dB = rt_gpu_alloc(n * 8), *dC = rt_gpu_alloc(n * 8);
    rt_gpu_copy_in(dA, x.a, n * 8);
    rt_gpu_copy_in(dB, x.b, n * 8);
    uint64_t args[4] = {m, (uint64_t)(uintptr_t)dA, (uint64_t)(uintptr_t)dB, (uint64_t)(uintptr_t)dC};
    rt_gpu_launch(fmm, (int64_t)n, 128, args, 4);
    rt_gpu_copy_out(g, dC, n * 8);
    rt_gpu_free(dA, n * 8); rt_gpu_free(dB, n * 8); rt_gpu_free(dC, n * 8);
    t1 = now_ms();
    if (t1 - t0 < *gpu) *gpu = t1 - t0;
  }
  *same = memcmp(g, x.c, n * 8) == 0;
}

static void line(const char *k, uint64_t size, uint64_t items, double par, double gpu, int same) {
  printf("%-14s %6llu %10llu items  par %9.3f ms  par/2 %9.3f ms  gpu %9.3f ms  %-6s %-6s identical %s\n", k,
         (unsigned long long)size, (unsigned long long)items, par, par / 2, gpu, gpu < par ? "GPU" : "CPU",
         gpu < par / 2 ? "GPU" : "CPU", same ? "yes" : "NO");
  fflush(stdout);
}

void marmot_main(void) {
  double t0 = now_ms();
  if (!rt_gpu_available()) { printf("no GPU\n"); return; }
  double t1 = now_ms();
  void *mod = rt_gpu_module(ptx, sizeof ptx);
  double t2 = now_ms();
  rt_gpu_info info;
  rt_gpu_info_get(&info);
  double t3 = now_ms();
  printf("start: driver+context %.1f ms, module %.1f ms, calibration %.1f ms; procs %d\n", t1 - t0, t2 - t1,
         t3 - t2, rt_gomaxprocs(0));
  fm = rt_gpu_function(mod, "mandel");
  fmm = rt_gpu_function(mod, "matmul");
  double par, gpu;
  int same;
  for (uint64_t w = 64; w <= 8192; w *= 2) {
    mandel(w, 50, &par, &gpu, &same);
    line("mandel it50", w, w * w, par, gpu, same);
  }
  for (uint64_t w = 32; w <= 2048; w *= 2) {
    mandel(w, 1000, &par, &gpu, &same);
    line("mandel it1000", w, w * w, par, gpu, same);
  }
  for (uint64_t m = 16; m <= 1024; m *= 2) {
    matmul(m, &par, &gpu, &same);
    line("matmul f64", m, m * m, par, gpu, same);
  }
}

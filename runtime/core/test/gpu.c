// GPU runtime: clean absence without CUDA; with a GPU, hand-written PTX
// kernels must match the CPU bit for bit (CPU side without FMA contraction).
// expect-stdout: ok
// env: GOMAXPROCS=4 MARMOT_GPU=1
#include "rtest.h"
#include <stdio.h>

#pragma clang fp contract(off)

static const char ptx[] = {
#embed "../gpu/kernels_test.ptx"
    , 0};

static uint64_t mix(uint64_t i, uint64_t seed) {
  uint64_t h = i * 0x9E3779B97F4A7C15ull + seed;
  h ^= h >> 29;
  h *= 0xBF58476D1CE4E5B9ull;
  h ^= h >> 32;
  return h;
}

static uint32_t mandel_px(uint64_t i, uint64_t w, uint64_t maxit, double sx, double ox, double sy, double oy) {
  double cr = (double)(i % w) * sx + ox, ci = (double)(i / w) * sy + oy, zr = 0, zi = 0;
  uint64_t k = 0;
  for (; k < maxit; k++) {
    double zr2 = zr * zr, zi2 = zi * zi;
    if (zr2 + zi2 > 4.0) break;
    double t = zr * zi;
    t = t + t;
    zi = t + ci;
    zr = (zr2 - zi2) + cr;
  }
  return (uint32_t)k;
}

static uint64_t u64f(double d) { uint64_t u; memcpy(&u, &d, 8); return u; }

void marmot_main(void) {
  if (!rt_gpu_available()) {
    rt_gpu_info info;
    CHECK(rt_gpu_info_get(&info) == 0 && info.present == 0);
    CHECK(rt_gpu_module(ptx, sizeof ptx) == 0 && rt_gpu_alloc(64) == 0);
    CHECK(rt_gpu_launch(0, 10, 0, 0, 0) == -1 && rt_gpu_should_offload(1 << 30, 100, 8, 8) == 0);
    CHECK(rt_gpu_break_even(10, 8, 8) == -1);
    rt_gpu_range rg = {0, 0, 0};
    CHECK(!rt_gpu_for(ptx, sizeof ptx, "saxpy_ctx", 100, &rg, 8, &rg, 1, 1000000));
    out("ok\n");
    return;
  }
  rt_gpu_info info;
  CHECK(rt_gpu_info_get(&info) == 1 && info.sm_count > 0 && info.mem_total > 0);
  void *mod = rt_gpu_module(ptx, sizeof ptx);
  CHECK(mod != 0 && rt_gpu_module(ptx, sizeof ptx) == mod);
  void *f_saxpy = rt_gpu_function(mod, "saxpy"), *f_hash = rt_gpu_function(mod, "ihash");
  void *f_mandel = rt_gpu_function(mod, "mandel"), *f_mm = rt_gpu_function(mod, "matmul");
  CHECK(f_saxpy && f_hash && f_mandel && f_mm && rt_gpu_function(mod, "nope") == 0);

  // Integers: hash of every index, several sizes including non-multiples
  // of the block size and a copy through the pinned staging path.
  int64_t sizes[] = {1, 255, 256, 257, 100000, 3000001};
  for (int s = 0; s < 6; s++) {
    int64_t n = sizes[s];
    uint64_t *h = rt_alloc((uintptr_t)n * 8, RT_ALLOC_NOSCAN, 0, 0);
    void *d = rt_gpu_alloc((uintptr_t)n * 8);
    CHECK(d);
    uint64_t args[2] = {12345, (uint64_t)(uintptr_t)d};
    CHECK(rt_gpu_launch(f_hash, n, 0, args, 2) == 0);
    CHECK(rt_gpu_copy_out(h, d, (uintptr_t)n * 8) == 0);
    for (int64_t i = 0; i < n; i++) CHECK(h[i] == mix((uint64_t)i, 12345));
    rt_gpu_free(d, (uintptr_t)n * 8);
  }
  // saxpy, float64: bit identical.
  int64_t n = 2000003;
  double *x = rt_alloc((uintptr_t)n * 8, RT_ALLOC_NOSCAN, 0, 0), *y = rt_alloc((uintptr_t)n * 8, RT_ALLOC_NOSCAN, 0, 0);
  double *g = rt_alloc((uintptr_t)n * 8, RT_ALLOC_NOSCAN, 0, 0);
  for (int64_t i = 0; i < n; i++) { x[i] = (double)i * 0.37 - 11.0; y[i] = 1.0 / ((double)i + 3.0); }
  void *dx = rt_gpu_alloc((uintptr_t)n * 8), *dy = rt_gpu_alloc((uintptr_t)n * 8);
  CHECK(rt_gpu_copy_in(dx, x, (uintptr_t)n * 8) == 0 && rt_gpu_copy_in(dy, y, (uintptr_t)n * 8) == 0);
  double a = 2.718281828459045;
  uint64_t sargs[3] = {u64f(a), (uint64_t)(uintptr_t)dx, (uint64_t)(uintptr_t)dy};
  CHECK(rt_gpu_launch(f_saxpy, n, 128, sargs, 3) == 0 && rt_gpu_sync() == 0);
  CHECK(rt_gpu_copy_out(g, dy, (uintptr_t)n * 8) == 0);
  for (int64_t i = 0; i < n; i++) CHECK(u64f(g[i]) == u64f(a * x[i] + y[i]));
  rt_gpu_free(dx, (uintptr_t)n * 8);
  rt_gpu_free(dy, (uintptr_t)n * 8);
  // Mandelbrot: iteration counts identical.
  uint64_t w = 640, hgt = 480, maxit = 500;
  double sx = 3.0 / 640, ox = -2.0, sy = 2.4 / 480, oy = -1.2;
  uint32_t *px = rt_alloc(w * hgt * 4, RT_ALLOC_NOSCAN, 0, 0);
  void *dp = rt_gpu_alloc(w * hgt * 4);
  uint64_t margs[7] = {w, maxit, u64f(sx), u64f(ox), u64f(sy), u64f(oy), (uint64_t)(uintptr_t)dp};
  CHECK(rt_gpu_launch(f_mandel, (int64_t)(w * hgt), 0, margs, 7) == 0);
  CHECK(rt_gpu_copy_out(px, dp, w * hgt * 4) == 0);
  for (uint64_t i = 0; i < w * hgt; i++) CHECK(px[i] == mandel_px(i, w, maxit, sx, ox, sy, oy));
  rt_gpu_free(dp, w * hgt * 4);
  // Matmul: sums in k order, identical.
  uint64_t m = 96;
  double *A = rt_alloc(m * m * 8, 0, 0, 0), *B = rt_alloc(m * m * 8, 0, 0, 0), *C = rt_alloc(m * m * 8, 0, 0, 0);
  for (uint64_t i = 0; i < m * m; i++) { A[i] = (double)(i % 17) * 0.1 + 0.3; B[i] = 1.0 / (double)(i % 23 + 1); }
  void *dA = rt_gpu_alloc(m * m * 8), *dB = rt_gpu_alloc(m * m * 8), *dC = rt_gpu_alloc(m * m * 8);
  rt_gpu_copy_in(dA, A, m * m * 8);
  rt_gpu_copy_in(dB, B, m * m * 8);
  uint64_t mmargs[4] = {m, (uint64_t)(uintptr_t)dA, (uint64_t)(uintptr_t)dB, (uint64_t)(uintptr_t)dC};
  CHECK(rt_gpu_launch(f_mm, (int64_t)(m * m), 0, mmargs, 4) == 0);
  CHECK(rt_gpu_copy_out(C, dC, m * m * 8) == 0);
  for (uint64_t r = 0; r < m; r++)
    for (uint64_t c = 0; c < m; c++) {
      double s = 0;
      for (uint64_t k = 0; k < m; k++) s = s + A[r * m + k] * B[k * m + c];
      CHECK(u64f(C[r * m + c]) == u64f(s));
    }
  // rt_gpu_for: ranges overlap (x is the first half of a buffer, y the
  // second half and also listed whole), pointers in ctx rewritten, the
  // reduction slot comes back, untouched bytes stay.
  {
    int64_t m2 = 1000003;
    double *buf = rt_alloc((uintptr_t)(2 * m2 + 1) * 8, RT_ALLOC_NOSCAN, 0, 0);
    double *exp = rt_alloc((uintptr_t)m2 * 8, RT_ALLOC_NOSCAN, 0, 0);
    for (int64_t i = 0; i < 2 * m2 + 1; i++) buf[i] = (double)i / 7.0;
    double av = 1.25;
    for (int64_t i = 0; i < m2; i++) exp[i] = av * buf[i] + buf[m2 + i];
    struct { double a; double *x; double *y; uint64_t count; } c = {av, buf, buf + m2, 5};
    rt_gpu_range rg[3] = {{buf, m2 * 8, 1}, {buf + m2, m2 * 8, 3}, {buf, (2 * m2 + 1) * 8, 1}};
    CHECK(rt_gpu_for(ptx, sizeof ptx, "saxpy_ctx", m2, &c, sizeof c, rg, 3, 100000));
    CHECK(c.count == 5 + (uint64_t)m2 && c.x == buf && c.y == buf + m2 && c.a == av);
    for (int64_t i = 0; i < m2; i++) CHECK(u64f(buf[m2 + i]) == u64f(exp[i]) && u64f(buf[i]) == u64f((double)i / 7.0));
    CHECK(buf[2 * m2] == (double)(2 * m2) / 7.0);
    // Too little work: the cost model declines.
    CHECK(!rt_gpu_for(ptx, sizeof ptx, "saxpy_ctx", 10, &c, sizeof c, rg, 3, 1));
  }
  // Pool reuse.
  void *p1 = rt_gpu_alloc(5000);
  rt_gpu_free(p1, 5000);
  CHECK(rt_gpu_alloc(6000) == p1);
  CHECK(rt_gpu_break_even(1000, 8, 8) > 0);
  fprintf(stderr, "gpu: %d SMs, cc %d.%d, launch %lld ns, h2d %.1f GB/s, d2h %.1f GB/s\n", info.sm_count,
          info.cc_major, info.cc_minor, (long long)info.launch_ns, info.h2d_gbps, info.d2h_gbps);
  out("ok\n");
}

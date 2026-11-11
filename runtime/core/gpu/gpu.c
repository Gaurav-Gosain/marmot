// gpu.c: GPU offload through the CUDA driver API, loaded at run time.
//
// Nothing links against CUDA: the first rt_gpu_* call dlopens libcuda and
// resolves the few entry points used here. Without the library, a device,
// or with MARMOT_GPU=0, every query reports no GPU and callers stay on the
// CPU. Offload decisions (rt_gpu_should_offload) are opt-in: they say yes
// only with MARMOT_GPU=1, until float results are settled with the loop
// compiler.
//
// One context (the device's primary context) is shared by all threads; it
// is made current on each entry because goroutines move between threads.
// Blocking driver calls are bracketed by rt_entersyscall/rt_exitsyscall so
// other goroutines keep running.
//
// Kernel convention (agreed with loops and backend): every kernel parameter
// is one 64-bit word (.u64, .s64, .f64, or a device pointer); parameter 0
// is the element count n; the kernel handles indexes [0, n) of a 1-D grid,
// returning early for global index >= n.

#include "../src/internal.h"

#include <dlfcn.h>

typedef int CUresult;
typedef int CUdevice;
typedef void *CUcontext;
typedef void *CUmodule;
typedef void *CUfunction;
typedef void *CUstream;
typedef unsigned long long CUdeviceptr;

#define CU_DEVICE_ATTRIBUTE_MULTIPROCESSOR_COUNT 16
#define CU_DEVICE_ATTRIBUTE_CLOCK_RATE 13
#define CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_MAJOR 75
#define CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_MINOR 76

static struct {
  CUresult (*Init)(unsigned);
  CUresult (*DeviceGet)(CUdevice *, int);
  CUresult (*DeviceGetCount)(int *);
  CUresult (*DeviceGetAttribute)(int *, int, CUdevice);
  CUresult (*DeviceTotalMem)(size_t *, CUdevice);
  CUresult (*PrimaryCtxRetain)(CUcontext *, CUdevice);
  CUresult (*CtxSetCurrent)(CUcontext);
  CUresult (*CtxSynchronize)(void);
  CUresult (*ModuleLoadData)(CUmodule *, const void *);
  CUresult (*ModuleGetFunction)(CUfunction *, CUmodule, const char *);
  CUresult (*MemAlloc)(CUdeviceptr *, size_t);
  CUresult (*MemFree)(CUdeviceptr);
  CUresult (*MemAllocHost)(void **, size_t);
  CUresult (*MemFreeHost)(void *);
  CUresult (*MemcpyHtoD)(CUdeviceptr, const void *, size_t);
  CUresult (*MemcpyDtoH)(void *, CUdeviceptr, size_t);
  CUresult (*MemcpyHtoDAsync)(CUdeviceptr, const void *, size_t, CUstream);
  CUresult (*MemcpyDtoHAsync)(void *, CUdeviceptr, size_t, CUstream);
  CUresult (*StreamSynchronize)(CUstream);
  CUresult (*MemGetInfo)(size_t *, size_t *);
  CUresult (*LaunchKernel)(CUfunction, unsigned, unsigned, unsigned, unsigned, unsigned, unsigned,
                           unsigned, CUstream, void **, void **);
} cu;

static rt_spin gpu_lock;
static int gpu_state;  // 0 not tried, 1 ready, 2 unavailable
static CUcontext ctx;
static CUdevice dev;
static int sm_count, cc_major, cc_minor, clock_khz;
static size_t mem_total;

// MARMOT_GPUTRACE=1: one stderr line per expensive step, with its time.
static int gpu_trace = -1;
static void trace(const char *what, int64_t t0) {
  if (gpu_trace < 0) {
    const char *e = rt_getenv("MARMOT_GPUTRACE");
    gpu_trace = e && e[0] == '1';
  }
  if (!gpu_trace) return;
  rt_print("gpu: ");
  rt_print(what);
  rt_print(" ");
  rt_printu((uint64_t)(os_nanotime() - t0) / 1000);
  rt_print(" us\n");
}

static bool resolve(void *h) {
#define SYM(field, name)                                       \
  do {                                                         \
    *(void **)&cu.field = dlsym(h, name);                      \
    if (!cu.field) return false;                               \
  } while (0)
  SYM(Init, "cuInit");
  SYM(DeviceGet, "cuDeviceGet");
  SYM(DeviceGetCount, "cuDeviceGetCount");
  SYM(DeviceGetAttribute, "cuDeviceGetAttribute");
  SYM(DeviceTotalMem, "cuDeviceTotalMem_v2");
  SYM(PrimaryCtxRetain, "cuDevicePrimaryCtxRetain");
  SYM(CtxSetCurrent, "cuCtxSetCurrent");
  SYM(CtxSynchronize, "cuCtxSynchronize");
  SYM(ModuleLoadData, "cuModuleLoadData");
  SYM(ModuleGetFunction, "cuModuleGetFunction");
  SYM(MemAlloc, "cuMemAlloc_v2");
  SYM(MemFree, "cuMemFree_v2");
  SYM(MemAllocHost, "cuMemAllocHost_v2");
  SYM(MemFreeHost, "cuMemFreeHost");
  SYM(MemcpyHtoD, "cuMemcpyHtoD_v2");
  SYM(MemcpyDtoH, "cuMemcpyDtoH_v2");
  SYM(MemcpyHtoDAsync, "cuMemcpyHtoDAsync_v2");
  SYM(MemcpyDtoHAsync, "cuMemcpyDtoHAsync_v2");
  SYM(StreamSynchronize, "cuStreamSynchronize");
  SYM(MemGetInfo, "cuMemGetInfo_v2");
  SYM(LaunchKernel, "cuLaunchKernel");
#undef SYM
  return true;
}

static bool gpu_init_locked(void) {
  const char *e = rt_getenv("MARMOT_GPU");
  if (e && e[0] == '0') return false;
  int64_t t0 = os_nanotime();
  void *h = dlopen("libcuda.so.1", RTLD_NOW | RTLD_LOCAL);
  if (!h) h = dlopen("libcuda.so", RTLD_NOW | RTLD_LOCAL);
  if (!h || !resolve(h)) return false;
  trace("dlopen", t0);
  t0 = os_nanotime();
  int n = 0;
  if (cu.Init(0) != 0 || cu.DeviceGetCount(&n) != 0 || n < 1) return false;
  trace("cuInit", t0);
  t0 = os_nanotime();
  if (cu.DeviceGet(&dev, 0) != 0) return false;
  cu.DeviceGetAttribute(&sm_count, CU_DEVICE_ATTRIBUTE_MULTIPROCESSOR_COUNT, dev);
  cu.DeviceGetAttribute(&cc_major, CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_MAJOR, dev);
  cu.DeviceGetAttribute(&cc_minor, CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_MINOR, dev);
  cu.DeviceGetAttribute(&clock_khz, CU_DEVICE_ATTRIBUTE_CLOCK_RATE, dev);
  cu.DeviceTotalMem(&mem_total, dev);
  if (cu.PrimaryCtxRetain(&ctx, dev) != 0) return false;
  cu.CtxSetCurrent(ctx);
  cu.CtxSynchronize();  // forces context creation now, so it is timed here
  trace("context", t0);
  return true;
}

// Ready the GPU for this thread; false if there is none.
static bool gpu_enter(void) {
  int s = __atomic_load_n(&gpu_state, __ATOMIC_ACQUIRE);
  if (s == 0) {
    rt_m *m = getm();
    if (m) m->locks++;
    spin_lock(&gpu_lock);
    if (gpu_state == 0) __atomic_store_n(&gpu_state, gpu_init_locked() ? 1 : 2, __ATOMIC_RELEASE);
    spin_unlock(&gpu_lock);
    if (m) m->locks--;
    s = gpu_state;
  }
  if (s != 1) return false;
  cu.CtxSetCurrent(ctx);
  return true;
}

RT_API int32_t rt_gpu_available(void) { return gpu_enter() ? 1 : 0; }

// ---- modules ----

typedef struct modent {
  const void *image;
  CUmodule mod;
} modent;
static modent mods[64];
static int nmods;

RT_API void *rt_gpu_module(const void *image, uintptr_t len) {
  (void)len;
  if (!gpu_enter()) return NULL;
  spin_lock(&gpu_lock);
  for (int i = 0; i < nmods; i++)
    if (mods[i].image == image) {
      CUmodule m = mods[i].mod;
      spin_unlock(&gpu_lock);
      return m;
    }
  spin_unlock(&gpu_lock);
  CUmodule mod = NULL;
  int64_t t0 = os_nanotime();
  rt_entersyscall();  // PTX JIT can take milliseconds
  CUresult r = cu.ModuleLoadData(&mod, image);
  rt_exitsyscall();
  trace("module load (PTX JIT unless cached)", t0);
  if (r != 0) return NULL;
  spin_lock(&gpu_lock);
  if (nmods < 64) mods[nmods++] = (modent){image, mod};
  spin_unlock(&gpu_lock);
  return mod;
}

RT_API void *rt_gpu_function(void *module, const char *name) {
  if (!module || !gpu_enter()) return NULL;
  CUfunction f = NULL;
  return cu.ModuleGetFunction(&f, module, name) == 0 ? f : NULL;
}

// ---- device memory pool: power-of-two classes from 4 KiB ----

#define POOL_CLASSES 40
typedef struct dbuf {
  struct dbuf *next;
  CUdeviceptr p;
} dbuf;
static dbuf *dpool[POOL_CLASSES];
static dbuf *dbuf_free;
static fixalloc dbuf_alloc;
static bool dbuf_inited;
static uint64_t pooled_bytes;

static int size_class(uintptr_t n) {
  int c = 0;
  uintptr_t s = 4096;
  while (s < n && c < POOL_CLASSES - 1) {
    s <<= 1;
    c++;
  }
  return c;
}

RT_API void *rt_gpu_alloc(uintptr_t bytes) {
  if (!gpu_enter()) return NULL;
  int c = size_class(bytes);
  spin_lock(&gpu_lock);
  dbuf *b = dpool[c];
  if (b) {
    dpool[c] = b->next;
    pooled_bytes -= (uint64_t)4096 << c;
    CUdeviceptr p = b->p;
    b->next = dbuf_free;
    dbuf_free = b;
    spin_unlock(&gpu_lock);
    return (void *)(uintptr_t)p;
  }
  spin_unlock(&gpu_lock);
  CUdeviceptr p = 0;
  if (cu.MemAlloc(&p, (size_t)4096 << c) != 0) {
    // Out of device memory: drop the pool and retry once.
    spin_lock(&gpu_lock);
    for (int k = 0; k < POOL_CLASSES; k++)
      while (dpool[k]) {
        dbuf *x = dpool[k];
        dpool[k] = x->next;
        cu.MemFree(x->p);
        x->next = dbuf_free;
        dbuf_free = x;
      }
    pooled_bytes = 0;
    spin_unlock(&gpu_lock);
    if (cu.MemAlloc(&p, (size_t)4096 << c) != 0) return NULL;
  }
  return (void *)(uintptr_t)p;
}

RT_API void rt_gpu_free(void *dptr, uintptr_t bytes) {
  if (!dptr || !gpu_enter()) return;
  int c = size_class(bytes);
  spin_lock(&gpu_lock);
  if (!dbuf_inited) {
    fixalloc_init(&dbuf_alloc, sizeof(dbuf));
    dbuf_inited = true;
  }
  dbuf *b = dbuf_free;
  if (b)
    dbuf_free = b->next;
  else
    b = fixalloc_alloc(&dbuf_alloc);
  b->p = (CUdeviceptr)(uintptr_t)dptr;
  b->next = dpool[c];
  dpool[c] = b;
  pooled_bytes += (uint64_t)4096 << c;
  spin_unlock(&gpu_lock);
}

// ---- copies ----
//
// Large copies go through two pinned staging buffers: the host copy of one
// chunk overlaps the DMA of the previous one (pageable copies stage
// through the driver's own buffer synchronously and are slower).

#define STAGE (4u << 20)
static void *stage[2];
static rt_mutex stage_mu;
static int copy_mode = -1;  // 0 staged (default), 1 direct pageable (MARMOT_GPU_COPY=direct)

static bool staged(void) {
  if (copy_mode < 0) {
    const char *e = rt_getenv("MARMOT_GPU_COPY");
    copy_mode = e && e[0] == 'd' ? 1 : 0;
  }
  return copy_mode == 0;
}

static bool stage_ready(void) {
  if (stage[0]) return true;
  void *a = NULL, *b = NULL;
  if (cu.MemAllocHost(&a, STAGE) != 0) return false;
  if (cu.MemAllocHost(&b, STAGE) != 0) {
    cu.MemFreeHost(a);
    return false;
  }
  stage[0] = a;
  stage[1] = b;
  return true;
}

RT_API int32_t rt_gpu_copy_in(void *dptr, const void *host, uintptr_t n) {
  if (!gpu_enter()) return -1;
  CUdeviceptr d = (CUdeviceptr)(uintptr_t)dptr;
  CUresult r = 0;
  rt_entersyscall();
  if (n >= 2 * STAGE && staged()) {
    rt_lock(&stage_mu);
    if (stage_ready()) {
      int k = 0;
      for (uintptr_t off = 0; off < n && r == 0; off += STAGE, k ^= 1) {
        uintptr_t len = n - off < STAGE ? n - off : STAGE;
        if (off >= 2 * STAGE) r = cu.CtxSynchronize();  // buffer k is free again
        __builtin_memcpy(stage[k], (const char *)host + off, len);
        if (r == 0) r = cu.MemcpyHtoDAsync(d + off, stage[k], len, NULL);
      }
      if (r == 0) r = cu.CtxSynchronize();
      rt_unlock(&stage_mu);
      rt_exitsyscall();
      return r == 0 ? 0 : -1;
    }
    rt_unlock(&stage_mu);
  }
  r = cu.MemcpyHtoD(d, host, n);
  rt_exitsyscall();
  return r == 0 ? 0 : -1;
}

RT_API int32_t rt_gpu_copy_out(void *host, const void *dptr, uintptr_t n) {
  if (!gpu_enter()) return -1;
  CUdeviceptr d = (CUdeviceptr)(uintptr_t)dptr;
  CUresult r = 0;
  rt_entersyscall();
  if (n >= 2 * STAGE && staged()) {
    rt_lock(&stage_mu);
    if (stage_ready()) {
      // DMA chunk i into stage[i&1] while copying chunk i-1 out.
      uintptr_t prev_off = 0, prev_len = 0;
      int k = 0;
      for (uintptr_t off = 0; off < n && r == 0; off += STAGE, k ^= 1) {
        uintptr_t len = n - off < STAGE ? n - off : STAGE;
        r = cu.MemcpyDtoHAsync(stage[k], d + off, len, NULL);
        if (r == 0 && prev_len) {
          // The previous chunk was synchronized below; copy it out.
          __builtin_memcpy((char *)host + prev_off, stage[k ^ 1], prev_len);
        }
        if (r == 0) r = cu.CtxSynchronize();
        prev_off = off;
        prev_len = len;
      }
      if (r == 0 && prev_len) __builtin_memcpy((char *)host + prev_off, stage[k ^ 1], prev_len);
      rt_unlock(&stage_mu);
      rt_exitsyscall();
      return r == 0 ? 0 : -1;
    }
    rt_unlock(&stage_mu);
  }
  r = cu.MemcpyDtoH(host, d, n);
  rt_exitsyscall();
  return r == 0 ? 0 : -1;
}

// ---- launch ----

RT_API int32_t rt_gpu_launch(void *fn, int64_t n, int32_t block, const uint64_t *args,
                             int32_t nargs) {
  if (!fn || n < 0 || nargs < 0 || nargs > 31 || !gpu_enter()) return -1;
  if (n == 0) return 0;
  if (block <= 0) block = 256;
  uint64_t words[32];
  void *params[32];
  words[0] = (uint64_t)n;
  params[0] = &words[0];
  for (int32_t i = 0; i < nargs; i++) {
    words[i + 1] = args[i];
    params[i + 1] = &words[i + 1];
  }
  uint64_t grid = ((uint64_t)n + (uint64_t)block - 1) / (uint64_t)block;
  if (grid > 0x7fffffffu) return -1;
  CUresult r = cu.LaunchKernel(fn, (unsigned)grid, 1, 1, (unsigned)block, 1, 1, 0, NULL, params, NULL);
  return r == 0 ? 0 : -1;
}

RT_API int32_t rt_gpu_sync(void) {
  if (!gpu_enter()) return -1;
  rt_entersyscall();
  CUresult r = cu.CtxSynchronize();
  rt_exitsyscall();
  return r == 0 ? 0 : -1;
}

// ---- cost model ----

// A tiny kernel to time launches.
static const char calib_ptx[] =
    ".version 7.0\n.target sm_52\n.address_size 64\n"
    ".visible .entry marmot_nop(.param .u64 n) { ret; }\n";

static int64_t launch_ns = -1;
static double h2d_gbps, d2h_gbps;

static void calibrate(void) {
  if (launch_ns >= 0) return;
  int64_t tc = os_nanotime();
  void *mod = rt_gpu_module(calib_ptx, sizeof calib_ptx);
  void *f = rt_gpu_function(mod, "marmot_nop");
  int64_t best = INT64_MAX;
  for (int i = 0; i < 20 && f; i++) {
    int64_t t0 = os_nanotime();
    rt_gpu_launch(f, 1, 32, NULL, 0);
    rt_gpu_sync();
    int64_t d = os_nanotime() - t0;
    if (d < best) best = d;
  }
  uintptr_t n = 16u << 20;
  void *host = os_map(n);
  void *d = rt_gpu_alloc(n);
  if (host && d) {
    __builtin_memset(host, 1, n);
    rt_gpu_copy_in(d, host, n);  // warm up: staging buffers, page tables
    rt_gpu_copy_out(host, d, n);
    int64_t t0 = os_nanotime();
    rt_gpu_copy_in(d, host, n);
    int64_t t1 = os_nanotime();
    rt_gpu_copy_out(host, d, n);
    int64_t t2 = os_nanotime();
    h2d_gbps = (double)n / (double)(t1 - t0);
    d2h_gbps = (double)n / (double)(t2 - t1);
  }
  if (d) rt_gpu_free(d, n);
  if (host) os_unmap(host, n);
  launch_ns = best == INT64_MAX ? 1000000 : best;
  trace("cost model calibration", tc);
}

RT_API int32_t rt_gpu_info_get(rt_gpu_info *out) {
  __builtin_memset(out, 0, sizeof *out);
  if (!gpu_enter()) return 0;
  calibrate();
  size_t fr = 0, tot = 0;
  cu.MemGetInfo(&fr, &tot);
  out->present = 1;
  out->sm_count = sm_count;
  out->cc_major = cc_major;
  out->cc_minor = cc_minor;
  out->clock_mhz = clock_khz / 1000;
  out->mem_total = mem_total;
  out->mem_free = fr + pooled_bytes;
  out->launch_ns = launch_ns;
  out->h2d_gbps = h2d_gbps;
  out->d2h_gbps = d2h_gbps;
  return 1;
}

// Predicted times, in ns, for n items each doing ops arithmetic operations
// and moving bytes_in / bytes_out bytes between host and device (0 when the
// data already lives on the device). CPU: all procs with rt_parallel_for at
// an assumed 2 operations per ns per core; GPU: 128 lanes per SM at the
// core clock, at 25% efficiency, plus launch and transfers.
static void estimate(int64_t n, double ops, double bin, double bout, double *gpu, double *cpu) {
  int32_t procs = atomic_load(&sched.nprocs);
  *cpu = (double)n * ops / (2.0 * procs) + 2000.0;
  double lanes = (double)sm_count * 128.0;
  double gops = lanes * (clock_khz / 1e6) * 0.25;  // operations per ns
  double h = h2d_gbps > 0 ? h2d_gbps : 5.0, d = d2h_gbps > 0 ? d2h_gbps : 5.0;
  *gpu = (double)launch_ns + (double)n * ops / gops + (double)n * bin / h + (double)n * bout / d;
}

// 1 when offloading n items is predicted to beat the CPU, only with a GPU
// and MARMOT_GPU=1 (opt-in while float semantics are being settled).
RT_API int32_t rt_gpu_should_offload(int64_t n, double ops, double bytes_in, double bytes_out) {
  const char *e = rt_getenv("MARMOT_GPU");
  if (!e || e[0] != '1' || !gpu_enter()) return 0;
  calibrate();
  double g, c;
  estimate(n, ops, bytes_in, bytes_out, &g, &c);
  return g < c ? 1 : 0;
}

// Smallest n (a power of two up to 2^40) at which the GPU is predicted to
// win, or -1 when it never does or there is no GPU.
RT_API int64_t rt_gpu_break_even(double ops, double bytes_in, double bytes_out) {
  if (!gpu_enter()) return -1;
  calibrate();
  for (int64_t n = 1024; n <= ((int64_t)1 << 40); n <<= 1) {
    double g, c;
    estimate(n, ops, bytes_in, bytes_out, &g, &c);
    if (g < c) return n;
  }
  return -1;
}

// ---- rt_gpu_for: the loop compiler's entry (docs/gpu.md) ----
//
// Runs the outlined parallel-for body as kernel kname(ptr ctx, i64 n), one
// thread per iteration, blocks of 256. Memory: the ranges (every byte the
// loop may touch) are merged where they overlap, copied to device buffers,
// and written ranges copied back; 8-byte words of ctx that point into a
// range are rewritten to the device copy (and restored afterwards, unless
// the kernel changed them). ctx itself is copied to the device and back
// (reduction slots). Returns false without doing anything when offload is
// off (opt-in MARMOT_GPU=1), there is no GPU, the cost model prefers the
// CPU, or any CUDA step fails before the launch; the caller then runs its
// CPU path.

typedef struct mrange {
  uintptr_t lo, hi;
  int64_t flags;
  void *dev;
} mrange;

static bool gpu_for_enabled(void) {
  const char *e = rt_getenv("MARMOT_GPU");
  return e && e[0] == '1';
}

RT_API bool rt_gpu_for(const char *ptx, int64_t ptxlen, const char *kname, int64_t n, void *ctx,
                       int64_t ctxsize, rt_gpu_range *ranges, int64_t nranges, int64_t cost) {
  if (n <= 0 || nranges < 0 || nranges > 64 || ctxsize < 0 || ctxsize > 4096) return false;
  if (!gpu_for_enabled() || !gpu_enter()) return false;
  // Merge ranges (insertion sort by base; at most 64).
  mrange mr[64];
  int nm = 0;
  uint64_t total = 0;
  for (int64_t i = 0; i < nranges; i++) {
    if (ranges[i].bytes <= 0) continue;
    mrange r = {(uintptr_t)ranges[i].base, (uintptr_t)ranges[i].base + (uintptr_t)ranges[i].bytes,
                ranges[i].flags, NULL};
    int j = nm++;
    while (j > 0 && mr[j - 1].lo > r.lo) {
      mr[j] = mr[j - 1];
      j--;
    }
    mr[j] = r;
  }
  int w = 0;
  for (int i = 0; i < nm; i++) {
    if (w > 0 && mr[i].lo <= mr[w - 1].hi) {
      if (mr[i].hi > mr[w - 1].hi) mr[w - 1].hi = mr[i].hi;
      mr[w - 1].flags |= mr[i].flags;
    } else {
      mr[w++] = mr[i];
    }
  }
  nm = w;
  uint64_t out_bytes = 0;
  for (int i = 0; i < nm; i++) {
    total += mr[i].hi - mr[i].lo;
    if (mr[i].flags & 2) out_bytes += mr[i].hi - mr[i].lo;
  }
  calibrate();
  double g, c;
  estimate(n, cost > 0 ? (double)cost : 1.0, (double)total / (double)n, (double)out_bytes / (double)n, &g, &c);
  if (g >= c) return false;

  void *mod = rt_gpu_module(ptx, (uintptr_t)ptxlen);
  void *fn = rt_gpu_function(mod, kname);
  if (!fn) return false;
  bool ok = true;
  int64_t tp = os_nanotime();
  for (int i = 0; i < nm && ok; i++) {
    uintptr_t len = mr[i].hi - mr[i].lo;
    mr[i].dev = rt_gpu_alloc(len);
    ok = mr[i].dev && rt_gpu_copy_in(mr[i].dev, (void *)mr[i].lo, len) == 0;
  }
  // ctx with pointers rewritten.
  uint8_t sent[4096];
  __builtin_memcpy(sent, ctx, (size_t)ctxsize);
  for (int64_t o = 0; o + 8 <= ctxsize; o += 8) {
    uintptr_t v;
    __builtin_memcpy(&v, sent + o, 8);
    for (int i = 0; i < nm; i++)
      if (v >= mr[i].lo && v < mr[i].hi) {
        v = (uintptr_t)mr[i].dev + (v - mr[i].lo);
        __builtin_memcpy(sent + o, &v, 8);
        break;
      }
  }
  void *dctx = ok ? rt_gpu_alloc(ctxsize > 0 ? (uintptr_t)ctxsize : 8) : NULL;
  ok = ok && dctx && rt_gpu_copy_in(dctx, sent, (uintptr_t)ctxsize) == 0;
  if (ok) {
    CUdeviceptr dp = (CUdeviceptr)(uintptr_t)dctx;
    int64_t nn = n;
    void *params[2] = {&dp, &nn};
    uint64_t grid = ((uint64_t)n + 255) / 256;
    ok = grid <= 0x7fffffffu &&
         cu.LaunchKernel(fn, (unsigned)grid, 1, 1, 256, 1, 1, 0, NULL, params, NULL) == 0;
  }
  trace("rt_gpu_for copy in + launch", tp);
  tp = os_nanotime();
  if (!ok) {
    for (int i = 0; i < nm; i++)
      if (mr[i].dev) rt_gpu_free(mr[i].dev, mr[i].hi - mr[i].lo);
    if (dctx) rt_gpu_free(dctx, (uintptr_t)ctxsize);
    return false;
  }
  // From here the work is done on the device: failures are fatal, since
  // the CPU path must not run again over partly written memory.
  if (rt_gpu_sync() != 0) rt_throw("GPU kernel failed");
  trace("rt_gpu_for kernel", tp);
  tp = os_nanotime();
  for (int i = 0; i < nm; i++) {
    if ((mr[i].flags & 2) &&
        rt_gpu_copy_out((void *)mr[i].lo, mr[i].dev, mr[i].hi - mr[i].lo) != 0)
      rt_throw("GPU copy back failed");
    rt_gpu_free(mr[i].dev, mr[i].hi - mr[i].lo);
  }
  if (ctxsize > 0) {
    uint8_t back[4096];
    if (rt_gpu_copy_out(back, dctx, (uintptr_t)ctxsize) != 0) rt_throw("GPU copy back failed");
    // Words the kernel did not change keep their host value (pointers).
    for (int64_t o = 0; o < ctxsize; o += 8) {
      int64_t len = ctxsize - o < 8 ? ctxsize - o : 8;
      if (__builtin_memcmp(back + o, sent + o, (size_t)len) != 0)
        __builtin_memcpy((uint8_t *)ctx + o, back + o, (size_t)len);
    }
  }
  rt_gpu_free(dctx, (uintptr_t)ctxsize);
  trace("rt_gpu_for copy out", tp);
  return true;
}

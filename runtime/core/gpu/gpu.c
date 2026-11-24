// gpu.c: GPU offload through the CUDA driver API, loaded at run time.
//
// Nothing links against CUDA: the first rt_gpu_* call dlopens libcuda and
// resolves the few entry points used here. Without the library, a device,
// or with MARMOT_GPU=0, every query reports no GPU and callers stay on the
// CPU. Loop offload (rt_gpu_for) is on in programs built with -gpu unless
// MARMOT_GPU=0. rt_gpu_should_offload stays opt-in (MARMOT_GPU=1).
//
// One context (the device's primary context) is shared by all threads; it
// is made current on each entry because goroutines move between threads.
// Blocking driver calls are bracketed by rt_entersyscall/rt_exitsyscall so
// other goroutines keep running.
//
// Kernel convention (shared with the compiler): every kernel parameter
// is one 64-bit word (.u64, .s64, .f64, or a device pointer); parameter 0
// is the element count n; the kernel handles indexes [0, n) of a 1-D grid,
// returning early for global index >= n.

#include "../src/internal.h"

#include <dlfcn.h>
#include <fcntl.h>
#include <stdio.h>
#include <sys/stat.h>
#include <unistd.h>

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
#define CU_DEVICE_ATTRIBUTE_KERNEL_EXEC_TIMEOUT 17

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
  CUresult (*DriverGetVersion)(int *);
  // Optional (cubin cache): PTX to cubin through the driver's linker.
  CUresult (*LinkCreate)(unsigned, int *, void **, void **);
  CUresult (*LinkAddData)(void *, int, void *, size_t, const char *, unsigned, int *, void **);
  CUresult (*LinkComplete)(void *, void **, size_t *);
  CUresult (*LinkDestroy)(void *);
} cu;
static int driver_version;

static rt_spin gpu_lock;  // module cache and buffer pool
static int gpu_state;  // 0 not tried, 1 ready, 2 unavailable, 3 in progress
static CUcontext ctx;
static CUdevice dev;
static int sm_count, cc_major, cc_minor, clock_khz, exec_timeout;
static size_t mem_total;

// MARMOT_GPUTRACE=1: one stderr line per expensive step, with its time.
static int gpu_trace = -1;
static bool gpu_trace_on(void) {
  if (gpu_trace < 0) {
    const char *e = rt_getenv("MARMOT_GPUTRACE");
    gpu_trace = e && e[0] == '1';
  }
  return gpu_trace;
}
static void trace(const char *what, int64_t t0) {
  if (!gpu_trace_on()) return;
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
  SYM(DriverGetVersion, "cuDriverGetVersion");
#undef SYM
  *(void **)&cu.LinkCreate = dlsym(h, "cuLinkCreate_v2");
  *(void **)&cu.LinkAddData = dlsym(h, "cuLinkAddData_v2");
  *(void **)&cu.LinkComplete = dlsym(h, "cuLinkComplete");
  *(void **)&cu.LinkDestroy = dlsym(h, "cuLinkDestroy");
  return true;
}

// MARMOT_GPU_FAULT=nolib|nodevice|badptx|oom|kernel|watchdog (tests): pretend
// that libcuda is missing, that there is no device, that every module fails to
// load, that device allocation fails, that a kernel fails after its launch, or
// that the device has a display watchdog (launches run in slices). Every case but kernel must end on the CPU path with the right
// result; kernel must be fatal.
static bool fault(const char *what) {
  const char *e = rt_getenv("MARMOT_GPU_FAULT");
  if (!e) return false;
  int i = 0;
  while (what[i] && e[i] == what[i]) i++;
  return what[i] == 0 && e[i] == 0;
}

static bool gpu_init_locked(void) {
  const char *e = rt_getenv("MARMOT_GPU");
  if (e && e[0] == '0') return false;
  int64_t t0 = os_nanotime();
  void *h = fault("nolib") ? NULL : dlopen("libcuda.so.1", RTLD_NOW | RTLD_LOCAL);
  if (!h && !fault("nolib")) h = dlopen("libcuda.so", RTLD_NOW | RTLD_LOCAL);
  if (!h || !resolve(h)) return false;
  trace("dlopen", t0);
  t0 = os_nanotime();
  int n = 0;
  if (cu.Init(0) != 0 || cu.DeviceGetCount(&n) != 0 || n < 1 || fault("nodevice")) return false;
  trace("cuInit", t0);
  t0 = os_nanotime();
  if (cu.DeviceGet(&dev, 0) != 0) return false;
  cu.DriverGetVersion(&driver_version);
  cu.DeviceGetAttribute(&sm_count, CU_DEVICE_ATTRIBUTE_MULTIPROCESSOR_COUNT, dev);
  cu.DeviceGetAttribute(&cc_major, CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_MAJOR, dev);
  cu.DeviceGetAttribute(&cc_minor, CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_MINOR, dev);
  cu.DeviceGetAttribute(&clock_khz, CU_DEVICE_ATTRIBUTE_CLOCK_RATE, dev);
  cu.DeviceGetAttribute(&exec_timeout, CU_DEVICE_ATTRIBUTE_KERNEL_EXEC_TIMEOUT, dev);
  cu.DeviceTotalMem(&mem_total, dev);
  if (cu.PrimaryCtxRetain(&ctx, dev) != 0) return false;
  cu.CtxSetCurrent(ctx);
  cu.CtxSynchronize();  // forces context creation now, so it is timed here
  trace("context", t0);
  return true;
}

// Wait until *st leaves state 3 (in progress), outside the P: CUDA setup
// takes 50 to 300 ms.
static void wait_done(int *st) {
  if (__atomic_load_n(st, __ATOMIC_ACQUIRE) != 3) return;
  rt_entersyscall();
  while (__atomic_load_n(st, __ATOMIC_ACQUIRE) == 3) os_usleep(200);
  rt_exitsyscall();
}

// Ready the GPU for this thread; false if there is none. The first caller
// (or the prewarm thread) initializes; others wait for it.
static bool gpu_enter(void) {
  int s = __atomic_load_n(&gpu_state, __ATOMIC_ACQUIRE);
  if (s == 0) {
    int z = 0;
    if (__atomic_compare_exchange_n(&gpu_state, &z, 3, false, __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
      rt_entersyscall();
      bool ok = gpu_init_locked();
      rt_exitsyscall();
      __atomic_store_n(&gpu_state, ok ? 1 : 2, __ATOMIC_RELEASE);
    }
  }
  wait_done(&gpu_state);
  if (__atomic_load_n(&gpu_state, __ATOMIC_ACQUIRE) != 1) return false;
  cu.CtxSetCurrent(ctx);
  return true;
}

RT_API int32_t rt_gpu_available(void) { return gpu_enter() ? 1 : 0; }

// ---- modules ----

// Loaded modules by image address. mod is NULL for an image that failed to
// load: it is not tried again (an old driver that rejects the PTX would
// otherwise repeat the JIT on every call). The table grows as needed.
typedef struct modent {
  const void *image;
  CUmodule mod;
} modent;
static modent *mods;
static int nmods, capmods;

// Room for one more entry in a table of n of cap elements of size sz;
// called with gpu_lock held. False when out of memory.
static bool table_room(void **tab, int n, int *cap, uintptr_t sz) {
  if (n < *cap) return true;
  int nc = *cap ? *cap * 2 : 64;
  void *p = os_map((uintptr_t)nc * sz);
  if (!p) return false;
  if (*tab) {
    __builtin_memcpy(p, *tab, (uintptr_t)n * sz);
    os_unmap(*tab, (uintptr_t)*cap * sz);
  }
  *tab = p;
  *cap = nc;
  return true;
}

// ---- cubin cache: PTX compiled once per (PTX, driver, device) ----
//
// $MARMOT_GPU_CACHE (default $XDG_CACHE_HOME/marmot/gpu or
// ~/.cache/marmot/gpu; "0" disables) holds HASH.cubin files. The driver's
// own JIT cache (~/.nv/ComputeCache) does the same job when it is enabled
// and large enough; this one does not depend on it.

static bool cache_dir(char *buf, int cap) {
  const char *e = rt_getenv("MARMOT_GPU_CACHE");
  if (e && e[0] == '0' && e[1] == 0) return false;
  const char *parts[3] = {0};
  if (e && e[0]) {
    parts[0] = e;
  } else if ((e = rt_getenv("XDG_CACHE_HOME")) && e[0]) {
    parts[0] = e, parts[1] = "/marmot/gpu";
  } else if ((e = rt_getenv("HOME")) && e[0]) {
    parts[0] = e, parts[1] = "/.cache/marmot/gpu";
  } else {
    return false;
  }
  int n = 0;
  for (int i = 0; i < 3 && parts[i]; i++)
    for (const char *q = parts[i]; *q; q++) {
      if (n >= cap - 40) return false;
      buf[n++] = *q;
    }
  buf[n] = 0;
  // mkdir -p
  for (int i = 1; i <= n; i++)
    if (buf[i] == '/' || buf[i] == 0) {
      char c = buf[i];
      buf[i] = 0;
      mkdir(buf, 0755);
      buf[i] = c;
    }
  return true;
}

static void hex64(char *out, uint64_t v) {
  for (int i = 15; i >= 0; i--, v >>= 4) out[i] = "0123456789abcdef"[v & 15];
}

// path = DIR/HASH.cubin for PTX image ptx of n bytes.
static bool cache_path(char *path, int cap, const void *ptx, uintptr_t n) {
  if (!cache_dir(path, cap)) return false;
  uint64_t h = rt_hash_bytes(ptx, (intptr_t)n, 0x6d61726d6f74ull);
  h = rt_hash_u64(h ^ (uint64_t)driver_version * 1000003u ^ (uint64_t)(cc_major * 10 + cc_minor), 0);
  int k = 0;
  while (path[k]) k++;
  path[k++] = '/';
  hex64(path + k, h);
  k += 16;
  __builtin_memcpy(path + k, ".cubin", 7);
  return true;
}

static void *read_file(const char *path, uintptr_t *size) {
  int fd = open(path, O_RDONLY | O_CLOEXEC);
  if (fd < 0) return NULL;
  struct stat st;
  void *buf = NULL;
  if (fstat(fd, &st) == 0 && st.st_size > 0) {
    *size = (uintptr_t)st.st_size;
    buf = os_map(*size);
    if (buf && read(fd, buf, *size) != (ssize_t)*size) {
      os_unmap(buf, *size);
      buf = NULL;
    }
  }
  close(fd);
  return buf;
}

static void write_file(const char *path, const void *data, uintptr_t n) {
  char tmp[600];
  int k = 0;
  while (path[k] && k < 560) tmp[k] = path[k], k++;
  __builtin_memcpy(tmp + k, ".tmp", 4);
  hex64(tmp + k + 4, (uint64_t)getpid());
  tmp[k + 20] = 0;
  int fd = open(tmp, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, 0644);
  if (fd < 0) return;
  bool ok = write(fd, data, n) == (ssize_t)n;
  close(fd);
  if (!ok || rename(tmp, path) != 0) unlink(tmp);
}

static bool is_ptx(const uint8_t *p) {
  // Not an ELF cubin (0x7f 'E') and not a fatbin (0x50ed55ba little endian).
  return !(p[0] == 0x7f && p[1] == 'E') && !(p[0] == 0x50 && p[1] == 0xed && p[2] == 0x55 && p[3] == 0xba);
}

static CUresult load_ptx(CUmodule *mod, void *image, uintptr_t len, const char *path);

// Blocking: JIT or cache read. Called with the P released.
static CUresult load_module(CUmodule *mod, const void *image, uintptr_t len) {
  char path[600];
  if (!is_ptx(image) || !cu.LinkCreate || !cu.LinkAddData || !cu.LinkComplete || !cu.LinkDestroy)
    return cu.ModuleLoadData(mod, image);
  if (len == 0) len = __builtin_strlen(image) + 1;
  if (!cache_path(path, sizeof path, image, len)) return cu.ModuleLoadData(mod, image);
  // The JIT wants NUL-terminated text: copy an image given without its NUL.
  void *text = (void *)image, *copy = NULL;
  if (((const char *)image)[len - 1] != 0) {
    copy = os_map(len + 1);
    if (!copy) return cu.ModuleLoadData(mod, image);
    __builtin_memcpy(copy, image, len);
    text = copy;
  }
  CUresult r = load_ptx(mod, text, copy ? len + 1 : len, path);
  if (copy) os_unmap(copy, len + 1);
  return r;
}

static CUresult load_ptx(CUmodule *mod, void *image, uintptr_t len, const char *path) {
  uintptr_t n = 0;
  int64_t t0 = os_nanotime();
  void *cubin = read_file(path, &n);
  if (cubin) {
    CUresult r = cu.ModuleLoadData(mod, cubin);
    os_unmap(cubin, n);
    trace("module from cubin cache", t0);
    if (r == 0) return 0;
    unlink(path);  // stale or corrupt
  }
  void *link = NULL;
  if (cu.LinkCreate(0, NULL, NULL, &link) != 0) return cu.ModuleLoadData(mod, image);
  void *out = NULL;
  size_t outn = 0;
  CUresult r = cu.LinkAddData(link, 1 /* CU_JIT_INPUT_PTX */, (void *)image, len, "marmot", 0, NULL, NULL);
  if (r == 0) r = cu.LinkComplete(link, &out, &outn);
  if (r == 0) r = cu.ModuleLoadData(mod, out);
  if (r == 0) write_file(path, out, outn);
  cu.LinkDestroy(link);
  trace("module JIT (cubin cache miss)", t0);
  return r == 0 ? 0 : cu.ModuleLoadData(mod, image);
}

RT_API void *rt_gpu_module(const void *image, uintptr_t len) {
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
  CUresult r = fault("badptx") ? 1 : load_module(&mod, image, len);
  rt_exitsyscall();
  trace(r == 0 ? "module load" : "module load failed (not tried again)", t0);
  if (r != 0) mod = NULL;
  spin_lock(&gpu_lock);
  for (int i = 0; i < nmods; i++)
    if (mods[i].image == image) {  // loaded by another thread meanwhile
      CUmodule m = mods[i].mod;
      spin_unlock(&gpu_lock);
      return m;
    }
  if (table_room((void **)&mods, nmods, &capmods, sizeof(modent))) mods[nmods++] = (modent){image, mod};
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
  if (!gpu_enter() || fault("oom")) return NULL;
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
static int calib_state;  // 0 not done, 1 done, 3 in progress

static void calibrate_once(void);

// The cost model's numbers; waits if another thread is measuring them.
static void calibrate(void) {
  int z = 0;
  if (__atomic_load_n(&calib_state, __ATOMIC_ACQUIRE) == 0 &&
      __atomic_compare_exchange_n(&calib_state, &z, 3, false, __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
    calibrate_once();
    __atomic_store_n(&calib_state, 1, __ATOMIC_RELEASE);
  }
  wait_done(&calib_state);
}

static void calibrate_once(void) {
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

// ---- rt_gpu_for: the loop compiler's entry ----
//
// Runs the outlined parallel-for body as kernel kname(ptr ctx, i64 base, i64
// count, i64 inner), one thread per iteration, blocks of 256. Memory: each range is
// copied to its own device buffer, mapped pointer words of ctx are moved to
// those buffers, and the written bytes of each range are copied back. See
// rt.h for the contract.

static int nreg;  // registered kernel images (rt_gpu_register, below)

// Loop offload is on in programs built with -gpu (they register their
// kernel images from a constructor, before main) unless MARMOT_GPU=0.
// MARMOT_GPU=1 turns it on for any caller (runtime tests call rt_gpu_for
// without a registered image).
static bool gpu_for_enabled(void) {
  const char *e = rt_getenv("MARMOT_GPU");
  if (e && e[0] == '0') return false;
  if (e && e[0] == '1') return true;
  return __atomic_load_n(&nreg, __ATOMIC_ACQUIRE) > 0;
}

// Test switches: MARMOT_GPU_WAIT=1 waits for setup instead of running early
// loops on the CPU; MARMOT_GPU_FORCE=1 skips the cost model.
static bool env_on(const char *name) {
  const char *e = rt_getenv(name);
  return e && e[0] == '1';
}

static void trace_cpu(const char *kname, const char *why) {
  if (!gpu_trace_on()) return;
  rt_print("gpu: ");
  rt_print(kname);
  rt_print(" on the CPU: ");
  rt_print(why);
  rt_print("\n");
}

// Modules registered at startup (rt_gpu_register), loaded by the prewarm
// thread so the first rt_gpu_for finds them compiled.
typedef struct regent {
  const void *image;
  uintptr_t len;
} regent;
// Constructors call rt_gpu_register before the runtime starts: the first
// 16 entries live in static storage, more go to mapped memory.
static regent reg_static[16];
static regent *reg = reg_static;
static int capreg = 16;
static int prewarm_busy;  // 1 while the prewarm thread runs

RT_API void rt_gpu_register(const void *image, uintptr_t len) {
  spin_lock(&gpu_lock);
  if (nreg == capreg) {
    int nc = capreg * 4;
    regent *p = os_map((uintptr_t)nc * sizeof(regent));
    if (p) {
      __builtin_memcpy(p, reg, (uintptr_t)nreg * sizeof(regent));
      if (reg != reg_static) os_unmap(reg, (uintptr_t)capreg * sizeof(regent));
      reg = p;
      capreg = nc;
    }
  }
  if (nreg < capreg) reg[nreg++] = (regent){image, len};
  spin_unlock(&gpu_lock);
}

static void prewarm_thread(void *arg) {
  (void)arg;
  if (gpu_enter()) {
    for (int i = 0; i < nreg; i++) rt_gpu_module(reg[i].image, reg[i].len);
    calibrate();
  }
  __atomic_store_n(&prewarm_busy, 0, __ATOMIC_RELEASE);
}

// Called at startup (entry.c, weak reference: only programs that link
// rt_gpu_for have it). When loop offload is on, CUDA setup and the cost model
// calibration (0.1 to 0.4 s) run on a separate thread while the program
// starts; rt_gpu_for leaves loops on the CPU until they are done.
void rt_gpu_prewarm(void) {
  if (!gpu_for_enabled()) return;
  __atomic_store_n(&prewarm_busy, 1, __ATOMIC_RELEASE);
  if (!os_thread_create(prewarm_thread, NULL, 256 << 10)) __atomic_store_n(&prewarm_busy, 0, __ATOMIC_RELEASE);
}

// True when setup and calibration are finished (or never started), so
// rt_gpu_for would not wait.
static bool gpu_settled(void) {
  return !__atomic_load_n(&prewarm_busy, __ATOMIC_ACQUIRE) &&
         __atomic_load_n(&gpu_state, __ATOMIC_ACQUIRE) != 3 &&
         __atomic_load_n(&calib_state, __ATOMIC_ACQUIRE) != 3;
}

RT_API bool rt_gpu_for(const char *ptx, int64_t ptxlen, const char *kname, int64_t n, void *ctx,
                       int64_t ctxsize, rt_gpu_range *ranges, int64_t nranges, int64_t cost,
                       rt_gpu_ptr *ptrs, int64_t nptrs, int64_t inner) {
  if (n <= 0 || nranges < 0 || nranges > 64 || nptrs < 0 || nptrs > 64 || ctxsize < 0 ||
      ctxsize > 4096 || inner < 0)
    return false;
  if (!gpu_for_enabled()) return false;
  // Every range must be a real object (a nil base would fault in the copy,
  // where the CPU path panics), and a written range must not overlap any other
  // one (the copy back would overwrite it). The loop compiler proves both; this
  // is the last line of defense.
  uint64_t in_bytes = 0, out_bytes = 0;
  for (int64_t i = 0; i < nranges; i++) {
    rt_gpu_range *r = &ranges[i];
    if (r->bytes < 0 || r->woff < 0 || r->wbytes < 0 || r->woff + r->wbytes > r->bytes) return false;
    if (r->bytes == 0) continue;
    if ((uintptr_t)r->base < 65536) {
      trace_cpu(kname, "a nil base");
      return false;
    }
    if (r->flags & 2)
      for (int64_t j = 0; j < nranges; j++) {
        uintptr_t lo = (uintptr_t)r->base, hi = lo + (uintptr_t)r->bytes;
        uintptr_t lo2 = (uintptr_t)ranges[j].base, hi2 = lo2 + (uintptr_t)ranges[j].bytes;
        if (j != i && ranges[j].bytes > 0 && lo < hi2 && lo2 < hi) {
          trace_cpu(kname, "a written range overlaps another range");
          return false;
        }
      }
    if (r->flags & 1 || r->woff != 0 || r->wbytes != r->bytes) in_bytes += (uint64_t)r->bytes;
    if (r->flags & 2) out_bytes += (uint64_t)r->wbytes;
  }
  for (int64_t k = 0; k < nptrs; k++)
    if (ptrs[k].off < 0 || ptrs[k].off + 8 > ctxsize || ptrs[k].range < 0 || ptrs[k].range >= nranges)
      return false;
  if (!gpu_settled()) {
    // Setup (0.1 to 0.5 s) is still running on the prewarm thread. Wait for it
    // when the CPU would take clearly longer than setup can (a big loop early in
    // main); otherwise run this call on the CPU.
    int32_t procs = atomic_load(&sched.nprocs);
    double cpu_ns = (double)n * (double)(cost > 0 ? cost : 1) / (2.0 * (procs > 0 ? procs : 1));
    if (!env_on("MARMOT_GPU_WAIT") && cpu_ns < 1e9) {
      trace_cpu(kname, "GPU setup not finished");
      return false;
    }
    rt_entersyscall();
    while (!gpu_settled()) os_usleep(500);
    rt_exitsyscall();
  }
  if (!gpu_enter()) return false;
  calibrate();
  double g, c;
  estimate(n, cost > 0 ? (double)cost : 1.0, (double)in_bytes / (double)n, (double)out_bytes / (double)n, &g, &c);
  if (g >= c && !env_on("MARMOT_GPU_FORCE")) {
    trace_cpu(kname, "the cost model predicts the CPU is faster");
    return false;
  }

  void *mod = rt_gpu_module(ptx, (uintptr_t)ptxlen);
  void *fn = rt_gpu_function(mod, kname);
  if (!fn) {
    trace_cpu(kname, "the kernel did not load");
    return false;
  }
  bool ok = true;
  int64_t tp = os_nanotime();
  void *dev[64] = {0};
  for (int64_t i = 0; i < nranges && ok; i++) {
    rt_gpu_range *r = &ranges[i];
    if (r->bytes == 0) continue;
    dev[i] = rt_gpu_alloc((uintptr_t)r->bytes);
    // A range written whole and never read needs no copy in.
    bool need_in = (r->flags & 1) || r->woff != 0 || r->wbytes != r->bytes;
    ok = dev[i] && (!need_in || rt_gpu_copy_in(dev[i], r->base, (uintptr_t)r->bytes) == 0);
  }
  // ctx with each mapped pointer word moved to the device copy of its range.
  // The word may point before or past the bytes the loop touches (y[k+i] with
  // the base y): the offset from the range start is kept as it is.
  uint8_t sent[4096];
  __builtin_memcpy(sent, ctx, (size_t)ctxsize);
  for (int64_t k = 0; k < nptrs; k++) {
    rt_gpu_range *r = &ranges[ptrs[k].range];
    uintptr_t v;
    __builtin_memcpy(&v, sent + ptrs[k].off, 8);
    v = (uintptr_t)dev[ptrs[k].range] + (v - (uintptr_t)r->base);
    __builtin_memcpy(sent + ptrs[k].off, &v, 8);
  }
  void *dctx = ok ? rt_gpu_alloc(ctxsize > 0 ? (uintptr_t)ctxsize : 8) : NULL;
  ok = ok && dctx && rt_gpu_copy_in(dctx, sent, (uintptr_t)ctxsize) == 0;
  // kernel(ctx, base, count). With a display watchdog (the driver kills a
  // kernel that runs for seconds) the loop runs in slices: the first sized
  // for about 5 ms by the cost model, each next one scaled from the time the
  // last one took, to about 50 ms.
  CUdeviceptr dp = (CUdeviceptr)(uintptr_t)dctx;
  int64_t base = 0, cnt = n;
  bool slice = exec_timeout || fault("watchdog");
  if (slice) {
    double ops_per_ns = (double)sm_count * 128.0 * (clock_khz / 1e6) * 0.25;
    double per_iter = (double)(cost > 0 ? cost : 1) / (ops_per_ns > 0 ? ops_per_ns : 1);
    cnt = (int64_t)(5e6 / (per_iter > 1e-3 ? per_iter : 1e-3));
    if (cnt < 256) cnt = 256;
  }
  bool launched = false;
  int slices = 0;
  while (ok && base < n) {
    if (cnt > n - base) cnt = n - base;
    void *params[4] = {&dp, &base, &cnt, &inner};
    uint64_t grid = ((uint64_t)cnt + 255) / 256;
    int64_t ts = os_nanotime();
    ok = grid <= 0x7fffffffu &&
         cu.LaunchKernel(fn, (unsigned)grid, 1, 1, 256, 1, 1, 0, NULL, params, NULL) == 0;
    if (!ok) {
      // Nothing ran yet: the CPU path may still run. After a slice, it may not.
      if (launched) rt_throw("GPU kernel launch failed");
      break;
    }
    launched = true;
    slices++;
    base += cnt;
    if (slice && base < n) {
      // From the first launch on, failures are fatal: the CPU path must not
      // run again over partly written memory.
      if (rt_gpu_sync() != 0) rt_throw("GPU kernel failed");
      int64_t dt = os_nanotime() - ts;
      if (dt < 1000) dt = 1000;
      double scale = 50e6 / (double)dt;
      if (scale > 8) scale = 8;
      int64_t next = (int64_t)((double)cnt * scale);
      cnt = next > 256 ? next : 256;
    }
  }
  trace("rt_gpu_for copy in + launch", tp);
  tp = os_nanotime();
  if (!ok) {
    for (int64_t i = 0; i < nranges; i++)
      if (dev[i]) rt_gpu_free(dev[i], (uintptr_t)ranges[i].bytes);
    if (dctx) rt_gpu_free(dctx, (uintptr_t)ctxsize);
    trace_cpu(kname, "a GPU step failed before the launch");
    return false;
  }
  if (rt_gpu_sync() != 0 || fault("kernel")) rt_throw("GPU kernel failed");
  trace("rt_gpu_for kernel", tp);
  if (gpu_trace_on()) {
    rt_print("gpu: ");
    rt_print(kname);
    rt_print(" on the GPU, ");
    rt_printu((uint64_t)n);
    rt_print(" iterations in ");
    rt_printu((uint64_t)slices);
    rt_print(" launches\n");
  }
  tp = os_nanotime();
  // Copy back exactly the bytes the loop writes: other goroutines may write
  // the bytes around them.
  for (int64_t i = 0; i < nranges; i++) {
    rt_gpu_range *r = &ranges[i];
    if (!dev[i]) continue;
    if ((r->flags & 2) && r->wbytes > 0 &&
        rt_gpu_copy_out((char *)r->base + r->woff, (char *)dev[i] + r->woff, (uintptr_t)r->wbytes) != 0)
      rt_throw("GPU copy back failed");
    rt_gpu_free(dev[i], (uintptr_t)r->bytes);
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

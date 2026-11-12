// Reference for rt_gpu_for (docs/gpu.md): loads PTX made by marmot-backend -gpu and runs it
// over registered host memory (zero copy; device address = host address under UVA). Run on
// Build on a Linux host with CUDA: gcc -I/opt/cuda/include rt_gpu_for_reference.c -L/opt/cuda/lib64 -lcuda. Verified
// 2026-11-12 on the RTX 3070 with the PTX of backend/test/driver/gpu.mlir: 16M saxpy, 0 wrong.
#include <cuda.h>
#include <stdio.h>
#include <stdlib.h>
#define CK(x) do { CUresult r = (x); if (r) { const char *s; cuGetErrorString(r, &s); printf("%s: %s\n", #x, s); return 1; } } while (0)
int main(void) {
  FILE *f = fopen("saxpy.ptx", "rb"); fseek(f, 0, SEEK_END); long len = ftell(f); rewind(f);
  char *ptx = calloc(1, len + 1); fread(ptx, 1, len, f);
  CUdevice dev; CUcontext ctx; CUmodule mod; CUfunction fn;
  CK(cuInit(0)); CK(cuDeviceGet(&dev, 0)); CK(cuCtxCreate(&ctx, NULL, 0, dev));
  CK(cuModuleLoadData(&mod, ptx)); CK(cuModuleGetFunction(&fn, mod, "saxpy_body_kernel"));
  long n = 1 << 24;
  double *x = malloc(n * 8), *y = malloc(n * 8);
  for (long i = 0; i < n; i++) { x[i] = i; y[i] = 1; }
  CK(cuMemHostRegister(x, n * 8, CU_MEMHOSTREGISTER_DEVICEMAP | CU_MEMHOSTREGISTER_PORTABLE));
  CK(cuMemHostRegister(y, n * 8, CU_MEMHOSTREGISTER_DEVICEMAP | CU_MEMHOSTREGISTER_PORTABLE));
  struct { double *x, *y; double a; } hctx = {x, y, 2.0};
  CUdeviceptr dctx; CK(cuMemAlloc(&dctx, sizeof hctx)); CK(cuMemcpyHtoD(dctx, &hctx, sizeof hctx));
  void *args[] = {&dctx, &n};
  CK(cuLaunchKernel(fn, (n + 255) / 256, 1, 1, 256, 1, 1, 0, 0, args, 0));
  CK(cuCtxSynchronize());
  long bad = 0;
  for (long i = 0; i < n; i++) if (y[i] != 2.0 * i + 1) bad++;
  printf("saxpy n=%ld wrong=%ld y[12345]=%.1f\n", n, bad, y[12345]);
  return bad != 0;
}

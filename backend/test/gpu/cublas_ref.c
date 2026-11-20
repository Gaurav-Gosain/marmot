// cuBLAS baseline for the GPU loop offload measurements: saxpy/daxpy and
// sgemm/dgemm with the copies that marmot's rt_gpu_for makes per call
// (pageable host memory to device and back). Prints cuBLAS handle creation
// apart, then the median over reps of: copy in, kernel (CUDA events), copy
// out. Default math mode (no TF32). Not part of any marmot build.
//
// Build on a Linux host with CUDA:
//   gcc -O2 -I/opt/cuda/include cublas_ref.c -L/opt/cuda/lib64 -lcublas -lcudart -o cublas_ref
// Run: ./cublas_ref saxpy|daxpy|sgemm|dgemm N [reps]
#include <cublas_v2.h>
#include <cuda_runtime.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#define CK(x) do { cudaError_t e = (x); if (e) { fprintf(stderr, "%s: %s\n", #x, cudaGetErrorString(e)); exit(1); } } while (0)
#define CB(x) do { cublasStatus_t s = (x); if (s) { fprintf(stderr, "%s: %d\n", #x, (int)s); exit(1); } } while (0)

static double now_ms(void) {
  struct timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return t.tv_sec * 1e3 + t.tv_nsec / 1e6;
}

static int cmp(const void *a, const void *b) {
  double x = *(const double *)a, y = *(const double *)b;
  return x < y ? -1 : x > y;
}

static double median(double *v, int n) {
  qsort(v, n, sizeof *v, cmp);
  return v[n / 2];
}

int main(int argc, char **argv) {
  if (argc < 3) {
    fprintf(stderr, "usage: cublas_ref saxpy|daxpy|sgemm|dgemm N [reps]\n");
    return 2;
  }
  const char *op = argv[1];
  long n = atol(argv[2]);
  int reps = argc > 3 ? atoi(argv[3]) : 5;
  int gemm = strstr(op, "gemm") != NULL, dbl = op[0] == 'd';
  size_t esz = dbl ? 8 : 4;
  size_t elems = gemm ? (size_t)n * n : (size_t)n;
  size_t bytes = elems * esz;
  char *ha = malloc(bytes), *hb = malloc(bytes), *hc = malloc(bytes);
  for (size_t i = 0; i < elems; i++) {
    if (dbl) {
      ((double *)ha)[i] = (double)(i % 7) + 0.5;
      ((double *)hb)[i] = (double)(i % 5) - 1.5;
    } else {
      ((float *)ha)[i] = (float)(i % 7) + 0.5f;
      ((float *)hb)[i] = (float)(i % 5) - 1.5f;
    }
  }
  CK(cudaFree(0));  // context
  double t0 = now_ms();
  cublasHandle_t h;
  CB(cublasCreate(&h));
  CB(cublasSetMathMode(h, CUBLAS_DEFAULT_MATH));
  double create_ms = now_ms() - t0;
  void *da, *db, *dc;
  CK(cudaMalloc(&da, bytes));
  CK(cudaMalloc(&db, bytes));
  CK(cudaMalloc(&dc, bytes));
  cudaEvent_t e0, e1;
  CK(cudaEventCreate(&e0));
  CK(cudaEventCreate(&e1));
  double *tin = calloc(reps, 8), *tk = calloc(reps, 8), *tout = calloc(reps, 8);
  for (int r = -1; r < reps; r++) {  // r = -1: warm up
    double a = now_ms();
    CK(cudaMemcpy(da, ha, bytes, cudaMemcpyHostToDevice));
    CK(cudaMemcpy(db, hb, bytes, cudaMemcpyHostToDevice));
    double b = now_ms();
    CK(cudaEventRecord(e0, 0));
    if (gemm) {
      if (dbl) {
        double one = 1, zero = 0;
        CB(cublasDgemm(h, CUBLAS_OP_N, CUBLAS_OP_N, n, n, n, &one, da, n, db, n, &zero, dc, n));
      } else {
        float one = 1, zero = 0;
        CB(cublasSgemm(h, CUBLAS_OP_N, CUBLAS_OP_N, n, n, n, &one, da, n, db, n, &zero, dc, n));
      }
    } else {
      if (dbl) {
        double al = 2;
        CB(cublasDaxpy(h, n, &al, da, 1, db, 1));
      } else {
        float al = 2;
        CB(cublasSaxpy(h, n, &al, da, 1, db, 1));
      }
    }
    CK(cudaEventRecord(e1, 0));
    CK(cudaEventSynchronize(e1));
    float kms;
    CK(cudaEventElapsedTime(&kms, e0, e1));
    double c = now_ms();
    CK(cudaMemcpy(hc, gemm ? dc : db, bytes, cudaMemcpyDeviceToHost));
    double d = now_ms();
    if (r >= 0) {
      tin[r] = b - a;
      tk[r] = kms;
      tout[r] = d - c;
    }
  }
  double k = median(tk, reps);
  double flops = gemm ? 2.0 * n * n * n : 2.0 * n;
  printf("%s n=%ld reps=%d create_ms=%.3f copy_in_ms=%.3f kernel_ms=%.4f copy_out_ms=%.3f gflops_kernel=%.1f\n", op, n,
         reps, create_ms, median(tin, reps), k, median(tout, reps), flops / (k * 1e6));
  cublasDestroy(h);
  return 0;
}

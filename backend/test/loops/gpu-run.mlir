// End to end on the GPU path: every loop the loop pass offers runs on the
// GPU (MARMOT_GPU_WAIT=1 waits for setup, MARMOT_GPU_FORCE=1 skips the cost
// model) and the output must equal stock Go's. Without a GPU every loop runs
// on the CPU and the output is the same; the trace checks then do not apply,
// so they run only where nvidia-smi finds a GPU.
// -loops-rtchecks: x and y in saxpyOff may alias without a run time check.
// Cases: a pointer below its object's range (y[k+i]), integer reductions,
// two field stores per element, sqrt (stays on the CPU: math.Sqrt is a call
// the loop pass does not accept yet), a strided writer next to
// another goroutine's writer (not offered: it would copy back the other
// goroutine's elements), a nil array pointer (the runtime refuses it, the CPU
// path panics and the panic is recovered), and float64 and float32 matmuls
// as 2-D kernels (one thread per element of c, bit-identical results).
// REQUIRES: nvptx
// RUN: MARMOT_BACKEND_FLAGS="-loops-rtchecks" %marmot build -nocache -release -gpu -o %t %S/Inputs/gpu_cases.go
// The embedded PTX is for sm_75 unless -gpu names another architecture.
// RUN: grep -a -q ".target sm_75" %t
// -gpu-remarks says which loops became kernels and why the others did not.
// RUN: MARMOT_BACKEND_FLAGS="-loops-rtchecks" %marmot build -release -gpu -gpu-remarks -o %t.r %S/Inputs/gpu_cases.go 2>&1 | %FileCheck %s --check-prefix=REMARK
// REMARK-DAG: loops: main.saxpyOff: {{.*}}GPU kernel
// REMARK-DAG: loops: main.evens: {{.*}}not offered to the GPU: stores leave gaps
// REMARK-DAG: loops: main.mmul64: {{.*}}GPU kernel (2-D)
// REMARK-DAG: loops: main.mmul32: {{.*}}GPU kernel (2-D)
// RUN: MARMOT_BACKEND_FLAGS="-loops-rtchecks" %marmot build -nocache -release -gpu=sm_86 -o %t.86 %S/Inputs/gpu_cases.go
// RUN: grep -a -q ".target sm_86" %t.86
// RUN: if %marmot build -gpu=volta -o %t.bad %S/Inputs/gpu_cases.go > %t.bad.out 2>&1; then exit 1; fi; grep -q "NVIDIA architecture" %t.bad.out
// RUN: if %marmot build -gpu -target wasip1/wasm -o %t.wasm %S/Inputs/gpu_cases.go > %t.wasm.out 2>&1; then exit 1; fi; grep -q "native target" %t.wasm.out
// RUN: MARMOT_GPU=0 %t > %t.cpu 2>&1 && diff %t.cpu %S/Inputs/gpu_cases.expected
// println writes to stderr, as the trace does: split them by the "gpu: " prefix.
// RUN: MARMOT_GPU=1 MARMOT_GPU_WAIT=1 MARMOT_GPU_FORCE=1 MARMOT_GPU_CACHE=0 MARMOT_GPUTRACE=1 %t > %t.trace 2>&1
// RUN: grep -v '^gpu: ' %t.trace > %t.gpu; diff %t.gpu %S/Inputs/gpu_cases.expected
// RUN: if nvidia-smi -L > /dev/null 2>&1; then %FileCheck %s < %t.trace; fi
// RUN: ! grep -q "evens.* on the GPU" %t.trace
// RUN: MARMOT_GPU=1 MARMOT_GPU_WAIT=1 MARMOT_GPU_FORCE=1 MARMOT_GPU_FAULT=watchdog %t > %t.wd 2>&1 && diff %t.wd %S/Inputs/gpu_cases.expected
// RUN: for f in nolib nodevice badptx oom; do MARMOT_GPU=1 MARMOT_GPU_WAIT=1 MARMOT_GPU_FORCE=1 MARMOT_GPU_FAULT=$f %t > %t.$f 2>&1 && diff %t.$f %S/Inputs/gpu_cases.expected || exit 1; done
// A kernel failure after the launch is fatal.
// RUN: if nvidia-smi -L > /dev/null 2>&1; then ! env MARMOT_GPU=1 MARMOT_GPU_WAIT=1 MARMOT_GPU_FORCE=1 MARMOT_GPU_FAULT=kernel %t > %t.k 2>&1 && grep -q "GPU kernel failed" %t.k; fi
// CHECK-DAG: saxpyOff{{.*}} on the GPU
// CHECK-DAG: reduce{{.*}} on the GPU
// CHECK-DAG: fill{{.*}} on the GPU
// CHECK-DAG: mmul64{{.*}} on the GPU, 36864 iterations
// CHECK-DAG: mmul32{{.*}} on the GPU, 36864 iterations

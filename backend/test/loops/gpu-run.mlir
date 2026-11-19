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
// path panics and the panic is recovered).
// REQUIRES: nvptx
// RUN: MARMOT_BACKEND_FLAGS="-gpu -loops-rtchecks" %marmot build -nocache -release -o %t %S/Inputs/gpu_cases.go
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

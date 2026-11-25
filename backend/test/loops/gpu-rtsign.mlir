// Loop nests with a run time size (c[i*n+j]): without -loops-rtsign the
// sign of the row stride 8n is unknown and the nest stays serial; with it
// the preheader checks 1 <= 8n and the nest runs in parallel, as a 2-D GPU
// kernel with -gpu. Output equals stock Go's either way.
// REQUIRES: nvptx
// RUN: MARMOT_BACKEND_FLAGS="-loops-rtchecks" %marmot build -nocache -release -gpu -gpu-remarks -o %t0 %S/Inputs/gpu_rtn.go 2>&1 | %FileCheck %s --check-prefix=OFF
// OFF-NOT: main.mmul: {{.*}}GPU kernel
// RUN: MARMOT_BACKEND_FLAGS="-loops-rtchecks -loops-rtsign" %marmot build -nocache -release -gpu -gpu-remarks -o %t %S/Inputs/gpu_rtn.go 2>&1 | %FileCheck %s --check-prefix=ON
// ON: main.mmul: {{.*}}parallel, 0 reductions, {{[0-9]+}} run time checks{{.*}}GPU kernel (2-D)
// RUN: MARMOT_GPU=0 %t > %t.cpu 2>&1 && diff %t.cpu %S/Inputs/gpu_rtn.expected
// RUN: MARMOT_GPU_WAIT=1 MARMOT_GPU_FORCE=1 MARMOT_GPUTRACE=1 %t > %t.trace 2>&1
// RUN: grep -v '^gpu: ' %t.trace > %t.gpu; diff %t.gpu %S/Inputs/gpu_rtn.expected
// RUN: if nvidia-smi -L > /dev/null 2>&1; then grep -q "mmul.* on the GPU" %t.trace; fi
// RUN: %t0 > %t.off 2>&1 && diff %t.off %S/Inputs/gpu_rtn.expected
// The CPU parallel path (-par) with the check.
// RUN: MARMOT_BACKEND_FLAGS="-par -loops-rtchecks -loops-rtsign -loops-remarks" %marmot build -nocache -release -o %t.par %S/Inputs/gpu_rtn.go 2>&1 | %FileCheck %s --check-prefix=PAR
// PAR: main.mmul: {{.*}}parallel
// RUN: %t.par > %t.par.out 2>&1 && diff %t.par.out %S/Inputs/gpu_rtn.expected

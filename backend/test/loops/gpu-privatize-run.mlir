// End to end: a histogram (privatized reduction array) built with -gpu runs
// right with MARMOT_GPU=1. Before the fix the loop was offered to the GPU and
// the kernel wrote through a host stack address. The program waits for the
// GPU setup first, so on a machine with a GPU the offer would be taken.
// Without a GPU or driver it runs on the CPU and must print the same.
// REQUIRES: nvptx
// RUN: MARMOT_BACKEND_FLAGS="-gpu -loops-privatize" %marmot build -nocache -release -o %t %S/Inputs/gpu_hist.go
// RUN: MARMOT_GPU=1 MARMOT_GPU_CACHE=0 %t > %t.out 2>&1 && diff %t.out %S/Inputs/gpu_hist.expected
// RUN: MARMOT_GPU=0 %t > %t.out0 2>&1 && diff %t.out0 %S/Inputs/gpu_hist.expected

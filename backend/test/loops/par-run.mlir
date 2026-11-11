// End to end: the loop passes on Go programs whose output must equal stock
// Go's. Inputs/par.go covers integer reductions (add, xor, min, max, count,
// wrapping sums), run time alias checks with overlapping slices, bounds check
// versioning (fast copy, and the slow copy panicking at the same index),
// loaded bounds ([][]T row lengths), row disjointness checks, reduction
// interchange (matmul, bit identical), and loops that must stay serial.
// RUN: MARMOT_BACKEND_FLAGS="-par" %S/../../../build/bin/marmot build -nocache -o %t %S/Inputs/par.go
// RUN: %t > %t.out 2>&1 && diff %t.out %S/Inputs/par.expected
// RUN: GOMAXPROCS=1 %t > %t.out1 2>&1 && diff %t.out1 %S/Inputs/par.expected
// RUN: MARMOT_BACKEND_FLAGS="-par -type-alias" %S/../../../build/bin/marmot build -nocache -o %t.ta %S/Inputs/par.go
// RUN: %t.ta > %t.ta.out 2>&1 && diff %t.ta.out %S/Inputs/par.expected
// Inputs/envp.go (probe7): its output depends on the environment, so compare
// against a build without the loop passes.
// RUN: MARMOT_BACKEND_FLAGS="-par -type-alias" %S/../../../build/bin/marmot build -nocache -o %t.env %S/Inputs/envp.go
// RUN: MARMOT_BACKEND_FLAGS="-no-loops" %S/../../../build/bin/marmot build -nocache -o %t.env0 %S/Inputs/envp.go
// RUN: %t.env > %t.env.out 2>&1 && %t.env0 > %t.env0.out 2>&1 && diff %t.env.out %t.env0.out

// RUN: %not %backend -o %t.o %S/Inputs/bad.mlir 2>&1 | %FileCheck %s --check-prefix=VERIFY
// RUN: %not %backend -o %t.o %S/Inputs/missing.mlir 2>&1 | %FileCheck %s --check-prefix=MISSING
// RUN: %not %backend -O7 -o %t.o %s 2>&1 | %FileCheck %s --check-prefix=OPT
// RUN: %not %backend -target riscv64-linux-gnu -o %t.o %s 2>&1 | %FileCheck %s --check-prefix=ARCH
// VERIFY: bad.mlir
// MISSING: missing.mlir
// OPT: bad -O level: 7
// ARCH: unsupported target architecture: riscv64
module {}

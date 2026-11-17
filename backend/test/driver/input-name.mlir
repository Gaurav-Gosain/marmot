// The object does not depend on the input's path or file name.
// RUN: rm -rf %t.d && mkdir -p %t.d/a %t.d/b
// RUN: cp %s %t.d/a/m.mlir && cp %s %t.d/b/another-name.mlir
// RUN: %backend -O2 -o %t.d/a.o %t.d/a/m.mlir
// RUN: %backend -O2 -o %t.d/b.o %t.d/b/another-name.mlir
// RUN: cmp %t.d/a.o %t.d/b.o
// RUN: %llvm-readobj --symbols %t.d/a.o | %FileCheck %s
// CHECK: Name: go-{{[0-9a-f]+}}
// CHECK-NEXT: Value: 0x0
// CHECK: Type: File
module {
  llvm.func @f(%x: i64) -> i64 {
    llvm.return %x : i64
  }
}

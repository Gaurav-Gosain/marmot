// Compile to an object, link with the system C library, run.
// RUN: %backend -o %t.o %s
// RUN: %clang %t.o -o %t.exe
// RUN: %t.exe | %FileCheck %s
// RUN: %backend -Oz -o %t.z.o %s
// RUN: %backend -Os -o %t.s.o %s
// RUN: %backend -O3 -o %t.3.o %s
// RUN: %backend -O1 -o %t.1.o %s
// RUN: %clang %t.z.o -o %t.z.exe && %t.z.exe | %FileCheck %s
// CHECK: hello from marmot
module {
  llvm.mlir.global internal constant @msg("hello from marmot\00") {addr_space = 0 : i32}
  llvm.func @puts(!llvm.ptr) -> i32
  llvm.func @main() -> i32 {
    %0 = llvm.mlir.addressof @msg : !llvm.ptr
    %1 = llvm.call @puts(%0) : (!llvm.ptr) -> i32
    %2 = llvm.mlir.constant(0 : i32) : i32
    llvm.return %2 : i32
  }
}

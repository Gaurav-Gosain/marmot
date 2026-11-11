// MLIR bytecode input works the same as text.
// RUN: %mlir-opt --emit-bytecode %s -o %t.mlirbc
// RUN: %backend -emit=llvm -o - %t.mlirbc | %FileCheck %s
// CHECK: define {{.*}}i32 @seven()
module {
  llvm.func @seven() -> i32 {
    %c = llvm.mlir.constant(7 : i32) : i32
    llvm.return %c : i32
  }
}

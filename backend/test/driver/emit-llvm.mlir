// RUN: %backend -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -O0 -emit=llvm -o - %s | %FileCheck %s --check-prefix=O0
// RUN: %backend -target x86_64-apple-macosx14.0 -emit=llvm -o - %s | %FileCheck %s --check-prefix=X86

// CHECK: target triple = "{{arm64|aarch64}}-apple-macosx
// CHECK: define {{.*}}i64 @{{"?}}main.add{{"?}}(i64 %0, i64 %1)
// CHECK-NOT: alloca
// CHECK: add i64
// O0: alloca i64
// X86: target triple = "x86_64-apple-macosx14.0"
module {
  llvm.func @"main.add"(%a: i64, %b: i64) -> i64 {
    %one = llvm.mlir.constant(1 : i64) : i64
    %p = llvm.alloca %one x i64 : (i64) -> !llvm.ptr
    %s = llvm.add %a, %b : i64
    llvm.store %s, %p : i64, !llvm.ptr
    %r = llvm.load %p : !llvm.ptr -> i64
    llvm.return %r : i64
  }
}

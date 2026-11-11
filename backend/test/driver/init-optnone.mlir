// Package initializers are compiled without optimization at -O1 and up.
// RUN: %backend -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -init-optnone=false -emit=llvm -o - %s | %FileCheck %s --check-prefix=OFF
// CHECK: define void @"example.com/p.init"() {{.*}}#[[INIT:[0-9]+]]
// CHECK: define void @"example.com/p.init#1"() {{.*}}#[[INIT]]
// CHECK: define void @"example.com/p.init$1"() {{.*}}#[[OTHER:[0-9]+]]
// CHECK: define void @"example.com/p.(*T).init"(ptr {{.*}}) {{.*}}#[[OTHER2:[0-9]+]]
// CHECK: attributes #[[INIT]] = { cold noinline {{.*}}optnone
// CHECK-NOT: attributes #[[OTHER]] = { {{.*}}optnone
// OFF-NOT: {{ }}optnone
module {
  llvm.mlir.global internal @x(0 : i64) : i64
  llvm.func @"example.com/p.init"() {
    %a = llvm.mlir.addressof @x : !llvm.ptr
    %c = llvm.mlir.constant(1 : i64) : i64
    llvm.store %c, %a : i64, !llvm.ptr
    llvm.return
  }
  llvm.func @"example.com/p.init#1"() {
    llvm.return
  }
  llvm.func @"example.com/p.init$1"() {
    llvm.return
  }
  llvm.func @"example.com/p.(*T).init"(%t: !llvm.ptr) {
    %c = llvm.mlir.constant(1 : i64) : i64
    llvm.store %c, %t : i64, !llvm.ptr
    llvm.return
  }
}

// Escape summaries cross modules in -wpo: the allocation passed to b.Read
// moves to the stack; the one passed to b.Pass (which hands it to c.Keep,
// which stores it) stays on the heap.
// RUN: printf '%s %t.a.ll\n%S/Inputs/wpo-esc-b.mlir %t.b.ll\n%S/Inputs/wpo-esc-c.mlir %t.c.ll\n' > %t.jobs
// RUN: %backend -O2 -emit=llvm -batch %t.jobs -wpo -time 2> %t.time
// RUN: %FileCheck %s < %t.a.ll
// RUN: %FileCheck %s --check-prefix=STATS < %t.time
// Without -wpo nothing is known about b.Read: both stay on the heap.
// RUN: %backend -O2 -emit=llvm -batch %t.jobs
// RUN: %FileCheck %s --check-prefix=SEP < %t.a.ll
// CHECK-LABEL: define {{.*}}@main(
// CHECK-COUNT-1: call {{.*}}@runtime.newobject
// CHECK-NOT: call {{.*}}@runtime.newobject
// CHECK: }
// STATS: escape rounds
// SEP-LABEL: define {{.*}}@main(
// SEP-COUNT-2: call {{.*}}@runtime.newobject
module {
  llvm.mlir.global external constant @"type:int"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  llvm.func @"b.Read"(!llvm.ptr) -> i64
  llvm.func @"b.Pass"(!llvm.ptr)
  llvm.func @main() -> i32 {
    %a = go.new @"type:int", i64
    %v = llvm.call @"b.Read"(%a) : (!llvm.ptr) -> i64
    %b = go.new @"type:int", i64
    llvm.call @"b.Pass"(%b) : (!llvm.ptr) -> ()
    %r = llvm.trunc %v : i64 to i32
    llvm.return %r : i32
  }
}

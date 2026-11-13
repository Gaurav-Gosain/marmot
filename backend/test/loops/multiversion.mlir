// Rule X: x86-64 versions of chunk functions, picked at run time.
// RUN: %backend -O2 -par -loops-mv=3 -target x86_64-unknown-linux-gnu -emit=llvm -o %t.ll %s
// RUN: %FileCheck %s < %t.ll
// RUN: %backend -O2 -par -loops-mv=4 -target x86_64-unknown-linux-gnu -emit=llvm -o %t4.ll %s
// RUN: %FileCheck %s --check-prefix=V4 < %t4.ll
// Off by default, and never on other targets or for a CPU that is not a baseline.
// RUN: %backend -O2 -par -target x86_64-unknown-linux-gnu -emit=llvm -o %t0.ll %s
// RUN: %FileCheck %s --check-prefix=OFF < %t0.ll
// RUN: %backend -O2 -par -loops-mv=3 -target aarch64-unknown-linux-gnu -emit=llvm -o %ta.ll %s
// RUN: %FileCheck %s --check-prefix=OFF < %ta.ll
// RUN: %backend -O2 -par -loops-mv=3 -target x86_64-unknown-linux-gnu -mcpu=skylake -emit=llvm -o %ts.ll %s
// RUN: %FileCheck %s --check-prefix=OFF < %ts.ll

// CHECK-LABEL: define void @scale(
// CHECK: %[[L:.*]] = {{.*}}call i32 @rt_x86_level()
// CHECK: %[[C:.*]] = icmp sgt i32 %[[L]], 2
// CHECK: %[[B:.*]] = select i1 %[[C]], ptr @"scale$par.v3", ptr @"scale$par"
// CHECK: call void @rt_parallel_for(i64 %{{.*}}, i64 %{{.*}}, ptr {{.*}}%[[B]], ptr
// CHECK: define internal void @"scale$par"({{.*}}) #[[A0:[0-9]+]]
// CHECK: define internal void @"scale$par.v3"({{.*}}) #[[A3:[0-9]+]]
// CHECK-DAG: attributes #[[A0]] = {{.*}}"target-cpu"="x86-64"
// CHECK-DAG: attributes #[[A3]] = {{.*}}"target-cpu"="x86-64-v3"

// V4: select i1 %{{.*}}, ptr @"scale$par.v4", ptr
// V4: define internal void @"scale$par.v4"

// OFF-NOT: rt_x86_level
// OFF-NOT: $par.v3

module {
  // dst[i] = src[i] * 3.0
  llvm.func @scale(%dst: !llvm.ptr {llvm.noalias}, %src: !llvm.ptr {llvm.noalias}, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c3 = llvm.mlir.constant(3.0 : f64) : f64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0 : i64), ^done
  ^loop(%i: i64):
    %a = llvm.getelementptr %src[%i] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    %v = llvm.load %a : !llvm.ptr -> f64
    %w = llvm.fmul %v, %c3 : f64
    %b = llvm.getelementptr %dst[%i] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    llvm.store %w, %b : f64, !llvm.ptr
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2 : i64), ^done
  ^done:
    llvm.return
  }
}

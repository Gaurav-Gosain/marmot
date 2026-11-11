// loop-parallelize decisions and the shape of the transformed code.
// RUN: %backend -O2 -par -loops-remarks -emit=llvm -o %t.ll %s 2>&1 | %FileCheck %s --check-prefix=REMARK
// RUN: %FileCheck %s --check-prefix=IR < %t.ll
// RUN: %backend -O2 -par -loops-remarks -emit=llvm -o %t3.ll %s 2>&1 | %FileCheck %s --check-prefix=SCAN
// RUN: %backend -O2 -no-par -loops-remarks -emit=llvm -o %t2.ll %s 2>&1 | %FileCheck %s --allow-empty --check-prefix=NOPAR
// RUN: %FileCheck %s --check-prefix=NOPARIR < %t2.ll

// REMARK: loops: sum: {{.*}}parallel, 1 reductions, 0 run time checks
// REMARK: loops: scale: {{.*}}parallel, 0 reductions, 1 run time checks
// REMARK: loops: recur: {{.*}}not parallel
// SCAN-DAG: loops: prefix: scan loop split into parallel blocks
// SCAN-DAG: loops: prefix: loop scan.p1{{.*}}parallel, 0 reductions
// SCAN-DAG: loops: prefix: loop scan.p2{{.*}}parallel, 0 reductions
// REMARK: loops: fsum: {{.*}}not parallel: {{.*}}floating point reduction
// REMARK: loops: search: {{.*}}not parallel: more than one exit
// REMARK: loops: callsout: {{.*}}not parallel: call to ext

// IR-LABEL: define i64 @sum(
// IR: call void @rt_parallel_for(i64 %{{.*}}, i64 %{{.*}}, ptr nonnull @"sum$par", ptr nonnull %par.ctx)
// IR-LABEL: define void @scale(
// IR: icmp ule
// IR: call void @rt_parallel_for
// IR-LABEL: define internal void @"sum$par"(ptr noalias %ctx, i64 %lo, i64 %hi)
// IR: atomicrmw add ptr
// IR-LABEL: define internal void @"scale$par"(

// NOPAR-NOT: loops:
// NOPARIR-NOT: rt_parallel_for

module {
  llvm.func @ext(i64)

  // sum of p[i] for i in [0, n)
  llvm.func @sum(%p: !llvm.ptr, %n: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0, %c0 : i64, i64), ^done(%c0 : i64)
  ^loop(%i: i64, %s: i64):
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %s2 = llvm.add %s, %v : i64
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2, %s2 : i64, i64), ^done(%s2 : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  }

  // dst[i] = src[i] * 3; dst and src may overlap: one run time check.
  llvm.func @scale(%dst: !llvm.ptr, %src: !llvm.ptr, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c3 = llvm.mlir.constant(3 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0 : i64), ^done
  ^loop(%i: i64):
    %a = llvm.getelementptr %src[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %w = llvm.mul %v, %c3 : i64
    %b = llvm.getelementptr %dst[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    llvm.store %w, %b : i64, !llvm.ptr
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2 : i64), ^done
  ^done:
    llvm.return
  }

  // p[i] += p[i-1]: a scan (Rule D), split into parallel blocks.
  llvm.func @prefix(%p: !llvm.ptr, %n: i64) {
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c1 : i64
    llvm.cond_br %z, ^loop(%c1 : i64), ^done
  ^loop(%i: i64):
    %im = llvm.sub %i, %c1 : i64
    %a = llvm.getelementptr %p[%im] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %b = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %u = llvm.load %b : !llvm.ptr -> i64
    %s = llvm.add %u, %v : i64
    llvm.store %s, %b : i64, !llvm.ptr
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2 : i64), ^done
  ^done:
    llvm.return
  }

  // p[i] = 3*p[i-1] + p[i]: loop carried, not a scan (the accumulator is
  // multiplied), so nothing happens.
  llvm.func @recur(%p: !llvm.ptr, %n: i64) {
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c3 = llvm.mlir.constant(3 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c1 : i64
    llvm.cond_br %z, ^loop(%c1 : i64), ^done
  ^loop(%i: i64):
    %im = llvm.sub %i, %c1 : i64
    %a = llvm.getelementptr %p[%im] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %v3 = llvm.mul %v, %c3 : i64
    %b = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %u = llvm.load %b : !llvm.ptr -> i64
    %s = llvm.add %u, %v3 : i64
    llvm.store %s, %b : i64, !llvm.ptr
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2 : i64), ^done
  ^done:
    llvm.return
  }

  // Strict floating point: the order of additions is kept.
  llvm.func @fsum(%p: !llvm.ptr, %n: i64) -> f64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %f0 = llvm.mlir.constant(0.0 : f64) : f64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0, %f0 : i64, f64), ^done(%f0 : f64)
  ^loop(%i: i64, %s: f64):
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    %v = llvm.load %a : !llvm.ptr -> f64
    %s2 = llvm.fadd %s, %v : f64
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2, %s2 : i64, f64), ^done(%s2 : f64)
  ^done(%r: f64):
    llvm.return %r : f64
  }

  // Early exit: the trip count depends on the data.
  llvm.func @search(%p: !llvm.ptr, %n: i64, %x: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %m1 = llvm.mlir.constant(-1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0 : i64), ^done(%m1 : i64)
  ^loop(%i: i64):
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %eq = llvm.icmp "eq" %v, %x : i64
    llvm.cond_br %eq, ^done(%i : i64), ^next
  ^next:
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2 : i64), ^done(%m1 : i64)
  ^done(%r: i64):
    llvm.return %r : i64
  }

  // A call with unknown effects.
  llvm.func @callsout(%n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.icmp "sgt" %n, %c0 : i64
    llvm.cond_br %z, ^loop(%c0 : i64), ^done
  ^loop(%i: i64):
    llvm.call @ext(%i) : (i64) -> ()
    %i2 = llvm.add %i, %c1 : i64
    %c = llvm.icmp "slt" %i2, %n : i64
    llvm.cond_br %c, ^loop(%i2 : i64), ^done
  ^done:
    llvm.return
  }
}

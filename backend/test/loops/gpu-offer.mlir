// GPU offer rules of the loop pass. A loop is a GPU kernel only when the
// runtime can copy each object to the device and copy back exactly the bytes
// the loop writes, and no pointer value moves through device memory.
// REQUIRES: nvptx
// RUN: %backend -O2 -par -gpu=sm_86 -loops-remarks -emit=llvm -o %t.ll %s 2>&1 | %FileCheck %s --check-prefix=REMARK
// RUN: %FileCheck %s < %t.ll
// REMARK-DAG: loops: cond_store: {{.*}}not offered to the GPU: a conditional store
// REMARK-DAG: loops: strided: {{.*}}not offered to the GPU: stores leave gaps
// REMARK-DAG: loops: ptrcopy: {{.*}}not offered to the GPU: the loop loads or stores pointer values
// REMARK-DAG: loops: saxpy_off: {{.*}}GPU kernel
// REMARK-DAG: loops: fields: {{.*}}GPU kernel
// The kernel's objects: x read (flags 1, nothing written), y read and written
// (flags 3); two pointer words of ctx mapped to them.
// CHECK-LABEL: define void @saxpy_off(
// CHECK: store i64 1, ptr
// CHECK: store i64 3, ptr
// CHECK: call i1 @rt_gpu_for({{.*}}, i64 2, i64 {{[0-9]+}}, ptr {{.*}}%par.ptrs{{.*}}, i64 2, i64 0)
// CHECK-LABEL: define void @fields(
// CHECK: call i1 @rt_gpu_for(
module {
  // if x[i] > 0 { y[i] = x[i] }
  llvm.func @cond_store(%x: !llvm.ptr {llvm.noalias}, %y: !llvm.ptr {llvm.noalias}, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %z = llvm.mlir.constant(0.0 : f64) : f64
    llvm.br ^h(%c0 : i64)
  ^h(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^b, ^d
  ^b:
    %xa = llvm.getelementptr inbounds %x[%i] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    %xv = llvm.load %xa : !llvm.ptr -> f64
    %pos = llvm.fcmp "ogt" %xv, %z : f64
    llvm.cond_br %pos, ^st, ^l
  ^st:
    %ya = llvm.getelementptr inbounds %y[%i] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    llvm.store %xv, %ya : f64, !llvm.ptr
    llvm.br ^l
  ^l:
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^h(%i1 : i64)
  ^d:
    llvm.return
  }
  // y[2*i] = 1
  llvm.func @strided(%y: !llvm.ptr {llvm.noalias}, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c2 = llvm.mlir.constant(2 : i64) : i64
    %one = llvm.mlir.constant(1.0 : f64) : f64
    llvm.br ^h(%c0 : i64)
  ^h(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^b, ^d
  ^b:
    %j = llvm.mul %i, %c2 : i64
    %ya = llvm.getelementptr inbounds %y[%j] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    llvm.store %one, %ya : f64, !llvm.ptr
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^h(%i1 : i64)
  ^d:
    llvm.return
  }
  // dst[i] = &src[i][1] on pointers (not a copy idiom)
  llvm.func @ptrcopy(%dst: !llvm.ptr {llvm.noalias}, %src: !llvm.ptr {llvm.noalias}, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    llvm.br ^h(%c0 : i64)
  ^h(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^b, ^d
  ^b:
    %sa = llvm.getelementptr inbounds %src[%i] : (!llvm.ptr, i64) -> !llvm.ptr, !llvm.ptr
    %v0 = llvm.load %sa : !llvm.ptr -> !llvm.ptr
    %v = llvm.getelementptr %v0[1] : (!llvm.ptr) -> !llvm.ptr, f64
    %da = llvm.getelementptr inbounds %dst[%i] : (!llvm.ptr, i64) -> !llvm.ptr, !llvm.ptr
    llvm.store %v, %da : !llvm.ptr, !llvm.ptr
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^h(%i1 : i64)
  ^d:
    llvm.return
  }
  // y[k+i] = a*x[i] + y[k+i]: y's range starts k elements past the pointer.
  llvm.func @saxpy_off(%a: f64, %x: !llvm.ptr {llvm.noalias}, %y: !llvm.ptr {llvm.noalias}, %k: i64, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    llvm.br ^h(%c0 : i64)
  ^h(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^b, ^d
  ^b:
    %xa = llvm.getelementptr inbounds %x[%i] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    %xv = llvm.load %xa : !llvm.ptr -> f64
    %ki = llvm.add %k, %i : i64
    %ya = llvm.getelementptr inbounds %y[%ki] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    %yv = llvm.load %ya : !llvm.ptr -> f64
    %m = llvm.fmul %a, %xv : f64
    %s = llvm.fadd %m, %yv : f64
    llvm.store %s, %ya : f64, !llvm.ptr
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^h(%i1 : i64)
  ^d:
    llvm.return
  }
  // y[i].a = 1; y[i].b = 2: two stores fill each element.
  llvm.func @fields(%y: !llvm.ptr {llvm.noalias}, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %one = llvm.mlir.constant(1.0 : f64) : f64
    %two = llvm.mlir.constant(2.0 : f64) : f64
    llvm.br ^h(%c0 : i64)
  ^h(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^b, ^d
  ^b:
    %fa = llvm.getelementptr inbounds %y[%i, 0] : (!llvm.ptr, i64) -> !llvm.ptr, !llvm.struct<(f64, f64)>
    llvm.store %one, %fa : f64, !llvm.ptr
    %fb = llvm.getelementptr inbounds %y[%i, 1] : (!llvm.ptr, i64) -> !llvm.ptr, !llvm.struct<(f64, f64)>
    llvm.store %two, %fb : f64, !llvm.ptr
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^h(%i1 : i64)
  ^d:
    llvm.return
  }
}

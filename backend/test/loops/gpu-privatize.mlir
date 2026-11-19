// GPU offer: a loop with a privatized reduction array (a histogram) is not
// offered to the GPU. The kernel cannot reach the array, which lives in the
// caller's frame; the loop stays a CPU parallel loop.
// REQUIRES: nvptx
// RUN: %backend -O2 -loops-privatize -gpu=sm_86 -loops-remarks -emit=llvm -o %t.ll %s 2>&1 | %FileCheck %s --check-prefix=REMARK
// RUN: %FileCheck %s < %t.ll
// REMARK: loops: hist: {{.*}}parallel, 0 reductions
// CHECK-LABEL: define {{.*}}@hist(
// CHECK-NOT: rt_gpu_for
// CHECK: call void @rt_parallel_for
// CHECK-NOT: rt_gpu_for
// CHECK-NOT: .ptx
module {
  llvm.func @hist(%b: !llvm.ptr {llvm.noalias}, %n: i64, %out: !llvm.ptr {llvm.noalias}) {
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %h = llvm.alloca %c1 x !llvm.array<256 x i64> : (i64) -> !llvm.ptr
    %z8 = llvm.mlir.constant(0 : i8) : i8
    %sz = llvm.mlir.constant(2048 : i64) : i64
    "llvm.intr.memset"(%h, %z8, %sz) <{isVolatile = false}> : (!llvm.ptr, i8, i64) -> ()
    %zero = llvm.mlir.constant(0 : i64) : i64
    llvm.br ^h(%zero : i64)
  ^h(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^body, ^done
  ^body:
    %bp = llvm.getelementptr inbounds %b[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i8
    %x = llvm.load %bp : !llvm.ptr -> i8
    %xi = llvm.zext %x : i8 to i64
    %hp = llvm.getelementptr inbounds %h[0, %xi] : (!llvm.ptr, i64) -> !llvm.ptr, !llvm.array<256 x i64>
    %v = llvm.load %hp : !llvm.ptr -> i64
    %v1 = llvm.add %v, %c1 : i64
    llvm.store %v1, %hp : i64, !llvm.ptr
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^h(%i1 : i64)
  ^done:
    "llvm.intr.memcpy"(%out, %h, %sz) <{isVolatile = false}> : (!llvm.ptr, !llvm.ptr, i64) -> ()
    llvm.return
  }
}

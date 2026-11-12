// GPU offload: a "marmot-gpu-body" function becomes an
// embedded PTX kernel and marmot_gpu_for becomes rt_gpu_for; without -gpu
// the call is false and the caller's CPU path runs.
// RUN: %backend -O2 -no-loops -gpu=sm_86 -emit=llvm -o %t.ll %s
// RUN: %FileCheck %s < %t.ll
// RUN: %FileCheck %s --check-prefix=REG < %t.ll
// RUN: %backend -O2 -no-loops -emit=llvm -o - %s | %FileCheck %s --check-prefix=CPU
// RUN: %backend -O0 -gpu -emit=llvm -o - %s | %FileCheck %s --check-prefix=CPU
// REG: @llvm.global_ctors = appending global {{.*}} @marmot.gpu.register
// REG: define internal void @marmot.gpu.register()
// REG-NEXT: entry:
// REG-NEXT: call void @rt_gpu_register(ptr {{.*}}@saxpy_body.ptx, i64 {{[0-9]+}})
// CPU-NOT: rt_gpu_register
// CHECK: @saxpy_body.ptx = private unnamed_addr constant {{.*}}.target sm_86
// CHECK-SAME: .entry saxpy_body_kernel
// CHECK-SAME: mul.rn.f64
// CHECK-NOT: fma.rn.f64
// CHECK: call i1 @rt_gpu_for(ptr {{.*}}@saxpy_body.ptx, i64 {{[0-9]+}}, ptr {{.*}}@saxpy_body.kname, i64 %{{.*}}, ptr %{{.*}}, i64 24, ptr %{{.*}}, i64 2, i64 4)
// CPU-NOT: rt_gpu_for
// CPU-NOT: marmot_gpu_for
// CPU: call void @rt_parallel_for
module {
  // ctx = {ptr x, ptr y, f64 a}; y[i] = a*x[i] + y[i] for i in [lo, hi)
  llvm.func @saxpy_body(%ctx: !llvm.ptr, %lo: i64, %hi: i64) attributes {passthrough = ["marmot-gpu-body"]} {
    %xp = llvm.load %ctx : !llvm.ptr -> !llvm.ptr
    %yq = llvm.getelementptr %ctx[0, 1] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(ptr, ptr, f64)>
    %yp = llvm.load %yq : !llvm.ptr -> !llvm.ptr
    %aq = llvm.getelementptr %ctx[0, 2] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(ptr, ptr, f64)>
    %a = llvm.load %aq : !llvm.ptr -> f64
    llvm.br ^h(%lo : i64)
  ^h(%i: i64):
    %more = llvm.icmp "slt" %i, %hi : i64
    llvm.cond_br %more, ^b, ^d
  ^b:
    %xa = llvm.getelementptr %xp[%i] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    %ya = llvm.getelementptr %yp[%i] : (!llvm.ptr, i64) -> !llvm.ptr, f64
    %xv = llvm.load %xa : !llvm.ptr -> f64
    %yv = llvm.load %ya : !llvm.ptr -> f64
    %m = llvm.fmul %a, %xv : f64
    %s = llvm.fadd %m, %yv : f64
    llvm.store %s, %ya : f64, !llvm.ptr
    %one = llvm.mlir.constant(1 : i64) : i64
    %i1 = llvm.add %i, %one : i64
    llvm.br ^h(%i1 : i64)
  ^d:
    llvm.return
  }
  llvm.func @marmot_gpu_for(!llvm.ptr, i64, !llvm.ptr, i64, !llvm.ptr, i64, i64) -> i1
  llvm.func @rt_parallel_for(i64, i64, !llvm.ptr, !llvm.ptr)
  llvm.func @saxpy(%ctx: !llvm.ptr, %n: i64, %ranges: !llvm.ptr) {
    %body = llvm.mlir.addressof @saxpy_body : !llvm.ptr
    %sz = llvm.mlir.constant(24 : i64) : i64
    %nr = llvm.mlir.constant(2 : i64) : i64
    %cost = llvm.mlir.constant(4 : i64) : i64
    %ok = llvm.call @marmot_gpu_for(%body, %n, %ctx, %sz, %ranges, %nr, %cost) : (!llvm.ptr, i64, !llvm.ptr, i64, !llvm.ptr, i64, i64) -> i1
    llvm.cond_br %ok, ^done, ^cpu
  ^cpu:
    %g = llvm.mlir.constant(1024 : i64) : i64
    llvm.call @rt_parallel_for(%n, %g, %body, %ctx) : (i64, i64, !llvm.ptr, !llvm.ptr) -> ()
    llvm.br ^done
  ^done:
    llvm.return
  }
}

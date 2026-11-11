// Parallel-for is opt-in (-par) until the loop optimizer's miscompiles of
// parallelized loops are fixed (.work/env/probe7.go: a loop calling an
// allocating function filled only element 0; sort_exact; std/exec/run).
// RUN: %backend -O2 -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -O3 -emit=llvm -o - %s | %FileCheck %s
// CHECK-NOT: rt_parallel_for
module {
  llvm.func @fill(%p: !llvm.ptr, %n: i64) {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    llvm.br ^h(%c0 : i64)
  ^h(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^b, ^d
  ^b:
    %a = llvm.getelementptr %p[%i] : (!llvm.ptr, i64) -> !llvm.ptr, i64
    %v = llvm.mul %i, %i : i64
    llvm.store %v, %a : i64, !llvm.ptr
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^h(%i1 : i64)
  ^d:
    llvm.return
  }
}

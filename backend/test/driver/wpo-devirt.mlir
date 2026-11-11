// -wpo: an indirect call whose type matches a single address-taken function
// in the whole program becomes a guarded direct call (inlined here); with the
// closed world the fallback is a trap.
// RUN: printf '%s %t.o\n' > %t.jobs
// RUN: %backend -O2 -emit=llvm -batch %t.jobs -wpo -time 2> %t.time
// RUN: mv %t.o %t.ll
// RUN: %FileCheck %s < %t.ll
// RUN: %FileCheck %s --check-prefix=STATS < %t.time
// RUN: %backend -O2 -emit=llvm -batch %t.jobs -wpo -no-closed-world
// RUN: mv %t.o %t2.ll
// RUN: %FileCheck %s --check-prefix=OPEN < %t2.ll
// STATS: 1 calls devirtualized
// CHECK-LABEL: define {{.*}}@main(
// CHECK-NOT: call {{.*}}%
// CHECK: llvm.trap
// OPEN-LABEL: define {{.*}}@main(
// OPEN: call i64 %
module {
  llvm.mlir.global internal @fv() : !llvm.ptr {
    %f = llvm.mlir.addressof @"p.inc" : !llvm.ptr
    llvm.return %f : !llvm.ptr
  }
  llvm.func @"p.inc"(%x: i64, %ctx: !llvm.ptr) -> i64 {
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.add %x, %c1 : i64
    llvm.return %r : i64
  }
  llvm.func @main() -> i32 {
    %g = llvm.mlir.addressof @fv : !llvm.ptr
    %f = llvm.load volatile %g : !llvm.ptr -> !llvm.ptr
    %c41 = llvm.mlir.constant(41 : i64) : i64
    %z = llvm.mlir.zero : !llvm.ptr
    %r = llvm.call %f(%c41, %z) : !llvm.ptr, (i64, !llvm.ptr) -> i64
    %t = llvm.trunc %r : i64 to i32
    llvm.return %t : i32
  }
}

// MARMOT_PARKCLEAR=1: spill slots dead at a park call are zeroed before it.
// Twelve pointers live across a call need more than the callee-saved
// registers, so some are spilled; all are dead at the selectgo call.
// RUN: env MARMOT_PARKCLEAR=1 %backend -O2 -target linux/arm64 -emit=asm -o - %s | %FileCheck %s --check-prefix=ARM64
// RUN: env MARMOT_PARKCLEAR=1 %backend -O2 -target linux/amd64 -emit=asm -o - %s | %FileCheck %s --check-prefix=AMD64
// RUN: %backend -O2 -target linux/arm64 -emit=asm -o - %s | %FileCheck %s --check-prefix=OFF
// ARM64-LABEL: main.spill:
// ARM64: bl use
// ARM64: xzr, [sp
// ARM64-NEXT: bl runtime.selectgo
// AMD64-LABEL: main.spill:
// AMD64: callq use
// AMD64: movq $0, {{[0-9]*}}(%rsp)
// AMD64: callq runtime.selectgo
// OFF-LABEL: main.spill:
// OFF-NOT: xzr, [sp
// OFF: bl runtime.selectgo
module {
  llvm.func @get(i64) -> !llvm.ptr
  llvm.func @sync()
  llvm.func @use(!llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr)
  llvm.func @"runtime.selectgo"(!llvm.ptr, i64) -> i64
  llvm.func @"main.spill"(%sel: !llvm.ptr) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %p0 = llvm.call @get(%c0) : (i64) -> !llvm.ptr
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %p1 = llvm.call @get(%c1) : (i64) -> !llvm.ptr
    %c2 = llvm.mlir.constant(2 : i64) : i64
    %p2 = llvm.call @get(%c2) : (i64) -> !llvm.ptr
    %c3 = llvm.mlir.constant(3 : i64) : i64
    %p3 = llvm.call @get(%c3) : (i64) -> !llvm.ptr
    %c4 = llvm.mlir.constant(4 : i64) : i64
    %p4 = llvm.call @get(%c4) : (i64) -> !llvm.ptr
    %c5 = llvm.mlir.constant(5 : i64) : i64
    %p5 = llvm.call @get(%c5) : (i64) -> !llvm.ptr
    %c6 = llvm.mlir.constant(6 : i64) : i64
    %p6 = llvm.call @get(%c6) : (i64) -> !llvm.ptr
    %c7 = llvm.mlir.constant(7 : i64) : i64
    %p7 = llvm.call @get(%c7) : (i64) -> !llvm.ptr
    %c8 = llvm.mlir.constant(8 : i64) : i64
    %p8 = llvm.call @get(%c8) : (i64) -> !llvm.ptr
    %c9 = llvm.mlir.constant(9 : i64) : i64
    %p9 = llvm.call @get(%c9) : (i64) -> !llvm.ptr
    %c10 = llvm.mlir.constant(10 : i64) : i64
    %p10 = llvm.call @get(%c10) : (i64) -> !llvm.ptr
    %c11 = llvm.mlir.constant(11 : i64) : i64
    %p11 = llvm.call @get(%c11) : (i64) -> !llvm.ptr
    llvm.call @sync() : () -> ()
    llvm.call @use(%p0, %p1, %p2, %p3, %p4, %p5, %p6, %p7, %p8, %p9, %p10, %p11) : (!llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr, !llvm.ptr) -> ()
    %one = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.call @"runtime.selectgo"(%sel, %one) : (!llvm.ptr, i64) -> i64
    %s = llvm.add %r, %one : i64
    llvm.return %s : i64
  }
}

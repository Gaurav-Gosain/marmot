// BCE must not change behavior: the guarded loop runs, the unguarded
// index still panics.
// RUN: %clang -c %S/Inputs/fakert.c -o %t.rt.o
// RUN: %backend -O2 -o %t.o %s
// RUN: %clang %t.o %t.rt.o -o %t.exe
// RUN: %t.exe 0 | %FileCheck %s --check-prefix=OK
// RUN: %not %t.exe 1 | %FileCheck %s --check-prefix=PANIC
// RUN: %backend -O2 -emit=mlir -o - %s | %FileCheck %s --check-prefix=IR
// OK: result 10
// PANIC: index out of range [5] with length 5
// IR-COUNT-1: llvm.call @runtime.panicBounds
module {
  llvm.mlir.global external constant @"type:int"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  // s := []int{0,1,2,3,4}; sum s[i] for i < len(s); if k == 1 { read s[len(s)] }
  llvm.func @test_main(%k: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c5 = llvm.mlir.constant(5 : i64) : i64
    %s = go.make_slice @"type:int", i64(%c5, %c5) : i64
    %p = llvm.extractvalue %s[0] : !llvm.struct<(ptr, i64, i64)>
    %n = llvm.extractvalue %s[1] : !llvm.struct<(ptr, i64, i64)>
    llvm.br ^fill(%c0 : i64)
  ^fill(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^fb, ^sum(%c0, %c0 : i64, i64)
  ^fb:
    %a = go.index %p[%i] len %n, i64 : i64
    llvm.store %i, %a : i64, !llvm.ptr
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^fill(%i1 : i64)
  ^sum(%j: i64, %acc: i64):
    %m2 = llvm.icmp "slt" %j, %n : i64
    llvm.cond_br %m2, ^sb, ^done
  ^sb:
    %b = go.index %p[%j] len %n, i64 : i64
    %v = llvm.load %b : !llvm.ptr -> i64
    %acc1 = llvm.add %acc, %v : i64
    %j1 = llvm.add %j, %c1 : i64
    llvm.br ^sum(%j1, %acc1 : i64, i64)
  ^done:
    %is1 = llvm.icmp "eq" %k, %c1 : i64
    llvm.cond_br %is1, ^bad, ^ret
  ^bad:
    %x = go.index %p[%n] len %n, i64 : i64
    %xv = llvm.load %x : !llvm.ptr -> i64
    llvm.return %xv : i64
  ^ret:
    llvm.return %acc : i64
  }
}

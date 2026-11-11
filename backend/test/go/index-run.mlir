// End to end: lower, compile, link with a fake runtime, run in and out of range.
// RUN: %clang -c %S/Inputs/fakert.c -o %t.rt.o
// RUN: %backend -O2 -o %t.o %s
// RUN: %clang %t.o %t.rt.o -o %t.exe
// RUN: %t.exe 2 | %FileCheck %s --check-prefix=OK
// RUN: %not %t.exe 7 | %FileCheck %s --check-prefix=IDX
// RUN: %not %t.exe -1 | %FileCheck %s --check-prefix=NEG
// RUN: %not %t.exe 6 | %FileCheck %s --check-prefix=SLICE
// RUN: %backend -O0 -o %t.0.o %s
// RUN: %clang %t.0.o %t.rt.o -o %t.0.exe
// RUN: %t.0.exe 2 | %FileCheck %s --check-prefix=OK
// RUN: %not %t.0.exe 7 | %FileCheck %s --check-prefix=IDX
// OK: result 30
// IDX: panic: index out of range [7] with length 5
// NEG: panic: index out of range [-1] with length 5
// SLICE: panic: slice bounds out of range [1:6] with capacity 5
module {
  llvm.mlir.global linkonce_odr constant @"type:int"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  // test_main(i): s := make([]int, 5); s[k] = 10*k for k < 5; t := s[1:i]
  // (only when i == 6, to test slice bounds); return s[i] + s[1]
  llvm.func @test_main(%i: i64) -> i64 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c5 = llvm.mlir.constant(5 : i64) : i64
    %c6 = llvm.mlir.constant(6 : i64) : i64
    %c10 = llvm.mlir.constant(10 : i64) : i64
    %s = go.make_slice @"type:int", i64(%c5, %c5) : i64
    %p = llvm.extractvalue %s[0] : !llvm.struct<(ptr, i64, i64)>
    %n = llvm.extractvalue %s[1] : !llvm.struct<(ptr, i64, i64)>
    llvm.br ^loop(%c0 : i64)
  ^loop(%k: i64):
    %more = llvm.icmp "slt" %k, %n : i64
    llvm.cond_br %more, ^body, ^done
  ^body:
    %a = go.index %p[%k] len %n, i64 : i64
    %v = llvm.mul %k, %c10 : i64
    llvm.store %v, %a : i64, !llvm.ptr
    %k1 = llvm.add %k, %c1 : i64
    llvm.br ^loop(%k1 : i64)
  ^done:
    %is6 = llvm.icmp "eq" %i, %c6 : i64
    llvm.cond_br %is6, ^slice, ^read
  ^slice:
    %t = go.slice %p[%c1 : %i : %n] cap %n, i64 : i64 -> !llvm.struct<(ptr, i64, i64)>
    llvm.br ^read
  ^read:
    %ai = go.index %p[%i] len %n, i64 : i64
    %vi = llvm.load %ai : !llvm.ptr -> i64
    %a1 = go.index %p[%c1] len %n, i64 : i64
    %v1 = llvm.load %a1 : !llvm.ptr -> i64
    %r = llvm.add %vi, %v1 : i64
    llvm.return %r : i64
  }
}

// A function referenced only from a linkonce itab must not be internalized:
// an imported body carries a copy of the itab into another module, where the
// slot load becomes a direct call.
// RUN: printf '%s %t.a.o\n%S/Inputs/wpo-itab-b.mlir %t.b.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -wpo
// RUN: %llvm-nm %t.b.o | %FileCheck %s
// RUN: %clang %t.a.o %t.b.o -o %t.exe
// RUN: %t.exe; test $? -eq 12
// CHECK: T {{_?}}b.T.M
module {
  llvm.func @"b.Call"(i64) -> i64
  llvm.func @main() -> i32 {
    %c7 = llvm.mlir.constant(7 : i64) : i64
    %r = llvm.call @"b.Call"(%c7) : (i64) -> i64
    %t = llvm.trunc %r : i64 to i32
    llvm.return %t : i32
  }
}

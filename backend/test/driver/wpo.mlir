// Whole-program batch mode: dead code removal, internalizing, imports.
// RUN: printf '%s %t.a.o\n%S/Inputs/wpo-b.mlir %t.b.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -wpo -time 2> %t.time
// RUN: %FileCheck %s --check-prefix=STATS < %t.time
// RUN: %llvm-nm %t.b.o | %FileCheck %s --check-prefix=B
// RUN: %llvm-nm %t.a.o | %FileCheck %s --check-prefix=A
// RUN: %clang %t.a.o %t.b.o -o %t.exe
// RUN: %t.exe; test $? -eq 37
// Without imports b.Small stays a call.
// RUN: %backend -O2 -batch %t.jobs -wpo -no-import
// RUN: %llvm-nm %t.a.o | %FileCheck %s --check-prefix=NOIMP
// STATS: wpo: {{[0-9]+}} functions, {{[0-9]+}} live, {{[1-9][0-9]*}} internalized, 1 imported
// B-NOT: b.Unused
// B-NOT: T {{_?}}b.helper
// B: T {{_?}}b.Count
// B: T {{_?}}b.Small
// B: T {{_?}}b.UsesHelper
// A-NOT: b.Small
// A: U {{_?}}b.Count
// NOIMP: U {{_?}}b.Small
module {
  llvm.func @"b.Small"(i64) -> i64
  llvm.func @"b.Count"() -> i64
  llvm.func @"b.UsesHelper"(i64) -> i64
  // main returns Small(4) + Count() + UsesHelper(12) = 12 + 1 + 24 = 37.
  llvm.func @main() -> i32 {
    %c4 = llvm.mlir.constant(4 : i64) : i64
    %c12 = llvm.mlir.constant(12 : i64) : i64
    %a = llvm.call @"b.Small"(%c4) : (i64) -> i64
    %b = llvm.call @"b.Count"() : () -> i64
    %c = llvm.call @"b.UsesHelper"(%c12) : (i64) -> i64
    %s1 = llvm.add %a, %b : i64
    %s2 = llvm.add %s1, %c : i64
    %r = llvm.trunc %s2 : i64 to i32
    llvm.return %r : i32
  }
}

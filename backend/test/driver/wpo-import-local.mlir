// Whole-program import of a body that uses a linkonce_odr function whose body
// reads a private constant of its module: the constant comes along. Before,
// the import source turned the constant into a declaration and the link of
// a.o failed with an undefined .str.1 (MARMOT_HOME_INSTANCES=1 -release).
// RUN: printf '%s %t.a.o\n%S/Inputs/wpo-import-local-b.mlir %t.b.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -wpo -time 2> %t.time
// RUN: %FileCheck %s --check-prefix=STATS < %t.time
// RUN: %llvm-nm %t.a.o | %FileCheck %s --check-prefix=A
// RUN: %clang %t.a.o %t.b.o -o %t.exe
// RUN: %t.exe; test $? -eq 105
// STATS: 1 imported
// A-NOT: U {{.*}}.str.1
module {
  llvm.func @"b.Small"(i64) -> i64
  // main returns Small(1) = 'h' + 1 = 105.
  llvm.func @main() -> i32 {
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %a = llvm.call @"b.Small"(%c1) : (i64) -> i64
    %r = llvm.trunc %a : i64 to i32
    llvm.return %r : i32
  }
}

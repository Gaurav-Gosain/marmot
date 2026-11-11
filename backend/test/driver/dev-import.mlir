// Dev batch: small functions of other modules of the same Go package
// (marmot.package) are imported available_externally and inlined; the cached
// object depends on the imported bodies only.
// RUN: rm -rf %t.cache %t.d && mkdir -p %t.d
// RUN: cp %s %t.d/a.mlir && cp %S/Inputs/dev-import-b.mlir %t.d/b.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n%t.d/b.mlir %t.b.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=COLD
// RUN: %llvm-nm %t.a.o | %FileCheck %s --check-prefix=INLINED
// RUN: %clang %t.a.o %t.b.o -o %t.exe && %t.exe; test $? -eq 12
// An edit of a body that a.mlir does not import: only b is compiled again.
// RUN: sed -i.bak 's/constant(7 : i64)/constant(8 : i64)/' %t.d/b.mlir
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=OTHER
// An edit of the imported body: a is compiled again and sees it.
// RUN: sed -i.bak 's/constant(3 : i64)/constant(5 : i64)/' %t.d/b.mlir
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=SMALL
// RUN: %clang %t.a.o %t.b.o -o %t.exe2 && %t.exe2; test $? -eq 20
// Another package: no import, the call stays.
// RUN: sed -i.bak 's/marmot.package = "p"/marmot.package = "q"/' %t.d/b.mlir
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache
// RUN: %llvm-nm %t.a.o | %FileCheck %s --check-prefix=CALL
// COLD: 1 imported{{.*}}compiled 2 of 2 objects
// INLINED-NOT: p.Small
// OTHER: compiled 1 of 2 objects
// SMALL: compiled 2 of 2 objects
// CALL: U {{_?}}p.Small
module attributes {marmot.package = "p"} {
  llvm.func @"p.Small"(i64) -> i64
  llvm.func @main() -> i32 {
    %c4 = llvm.mlir.constant(4 : i64) : i64
    %a = llvm.call @"p.Small"(%c4) : (i64) -> i64
    %r = llvm.trunc %a : i64 to i32
    llvm.return %r : i32
  }
}

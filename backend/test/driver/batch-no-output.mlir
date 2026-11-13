// Batch job output "-": the module takes part (its bodies are imported by
// the others) but gets no output, as marmot build -emit llvm asks for every
// module but main's. The wanted output is the same as with all outputs.
// RUN: rm -rf %t.cache %t.d && mkdir -p %t.d && cd %t.d
// RUN: cp %s %t.d/a.mlir && cp %S/Inputs/dev-import-b.mlir %t.d/b.mlir
// RUN: printf '%t.d/a.mlir %t.all.ll\n%t.d/b.mlir %t.b.ll\n' > %t.all
// RUN: %backend -O2 -emit=llvm -batch %t.all -cache-dir %t.cache
// RUN: printf '%t.d/a.mlir %t.one.ll\n%t.d/b.mlir -\n' > %t.one
// RUN: %backend -O2 -emit=llvm -batch %t.one -cache-dir %t.cache -time 2> %t.time > %t.stdout
// RUN: %FileCheck %s --check-prefix=TIME < %t.time
// RUN: test ! -e %t.d/- && test ! -s %t.stdout
// RUN: diff %t.all.ll %t.one.ll
// RUN: %FileCheck %s < %t.one.ll
// TIME: 1 imported
// TIME: 2 job(s)
// CHECK: define{{.*}}@main
// CHECK-NOT: p.Small
module attributes {marmot.package = "p"} {
  llvm.func @"p.Small"(i64) -> i64
  llvm.func @main() -> i32 {
    %c4 = llvm.mlir.constant(4 : i64) : i64
    %a = llvm.call @"p.Small"(%c4) : (i64) -> i64
    %r = llvm.trunc %a : i64 to i32
    llvm.return %r : i32
  }
}

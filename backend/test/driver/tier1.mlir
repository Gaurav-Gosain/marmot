// Background tier 1: a tier0 batch with -tier1-spool leaves each module it
// compiles in the spool; -tier1-run compiles them at -O2 into the cache and
// empties the spool; the next batch takes the -O2 object. A batch without
// -tier1-spool, or with other options, keeps its tier0 objects.
// UNSUPPORTED: aarch64
// RUN: rm -rf %t.cache %t.spool %t.d && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: %backend -tier0 -batch %t.jobs -cache-dir %t.cache -tier1-spool %t.spool -time 2>&1 | %FileCheck %s --check-prefix=COLD
// RUN: test $(ls %t.spool/*.mlir | wc -l) -eq 1
// RUN: cp %t.a.o %t.t0.o
// RUN: %backend -tier0 -cache-dir %t.cache -tier1-spool %t.spool -tier1-run
// RUN: test $(ls %t.spool | grep -c mlir) -eq 0
// RUN: %backend -tier0 -batch %t.jobs -cache-dir %t.cache -tier1-spool %t.spool -time 2>&1 | %FileCheck %s --check-prefix=WARM
// RUN: %not cmp -s %t.a.o %t.t0.o
// RUN: %clang %t.a.o -o %t.exe && %t.exe; test $? -eq 42
// RUN: test $(ls %t.spool | grep -c mlir) -eq 0
// Without -tier1-spool the batch keeps the tier0 object.
// RUN: %backend -tier0 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=OFF
// RUN: cmp %t.a.o %t.t0.o
// A runner with other options drops the module: no object for the batch.
// RUN: %backend -tier0 -keep-call-sites -batch %t.jobs -cache-dir %t.cache -tier1-spool %t.spool
// RUN: test $(ls %t.spool/*.mlir | wc -l) -eq 1
// RUN: %backend -tier0 -cache-dir %t.cache -tier1-spool %t.spool -tier1-run
// RUN: test $(ls %t.spool | grep -c mlir) -eq 0
// RUN: %backend -tier0 -keep-call-sites -batch %t.jobs -cache-dir %t.cache -tier1-spool %t.spool -time 2>&1 | %FileCheck %s --check-prefix=OFF
// RUN: %not %backend -tier1-run 2>&1 | %FileCheck %s --check-prefix=USAGE
// COLD: compiled 1 of 1 objects
// COLD: 0 tier 1 objects
// WARM: compiled 0 of 1 objects
// WARM: 1 tier 1 objects
// OFF: 0 tier 1 objects
// USAGE: -tier1-run needs -tier0, -tier1-spool and -cache-dir
module {
  llvm.mlir.global internal @counter(40 : i64) : i64
  llvm.func internal @bump() -> i64 {
    %p = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %p : !llvm.ptr -> i64
    %two = llvm.mlir.constant(2 : i64) : i64
    %w = llvm.add %v, %two : i64
    llvm.store %w, %p : i64, !llvm.ptr
    llvm.return %w : i64
  }
  llvm.func @main() -> i32 {
    %b = llvm.call @bump() : () -> i64
    %r = llvm.trunc %b : i64 to i32
    llvm.return %r : i32
  }
}

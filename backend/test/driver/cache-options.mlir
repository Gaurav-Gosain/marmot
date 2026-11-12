// Every option that can change generated code is part of the cache key, also
// options read directly by libraries (here an LLVM option and a loop optimizer
// option) and options given through MARMOT_BACKEND_FLAGS.
// RUN: rm -rf %t.cache && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=MISS
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -j 1 -time 2>&1 | %FileCheck %s --check-prefix=HIT
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -enable-tail-merge=false -time 2>&1 | %FileCheck %s --check-prefix=MISS
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -enable-tail-merge=false -time 2>&1 | %FileCheck %s --check-prefix=HIT
// RUN: env MARMOT_BACKEND_FLAGS="-loops-force-par" %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=MISS
// RUN: env MARMOT_BACKEND_FLAGS="-loops-force-par" %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=HIT
// MISS: compiled 1 of 1 objects
// HIT: compiled 0 of 1 objects
module {
  llvm.func @f(%x: i64) -> i64 {
    %c = llvm.mlir.constant(3 : i64) : i64
    %r = llvm.mul %x, %c : i64
    llvm.return %r : i64
  }
}

// The object cache keys on the backend binary's build id: a different
// marmot-backend (here the same binary with another build id) does not reuse
// the objects of the first one, and each binary still hits its own entries.
// UNSUPPORTED: arm64
// RUN: rm -rf %t.cache && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: cp %backend %t.be
// RUN: python3 %S/Inputs/patch_build_id.py %t.be
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=MISS
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=HIT
// RUN: %t.be -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=MISS
// RUN: %t.be -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=HIT
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=HIT
// MISS: compiled 1 of 1 objects
// HIT: compiled 0 of 1 objects
module {
  llvm.func @f(%x: i64) -> i64 {
    %c = llvm.mlir.constant(3 : i64) : i64
    %r = llvm.mul %x, %c : i64
    llvm.return %r : i64
  }
}

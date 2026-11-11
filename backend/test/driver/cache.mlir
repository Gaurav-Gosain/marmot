// The batch cache: a warm run computes nothing; a changed module misses only
// what depends on it.
// RUN: rm -rf %t.cache && mkdir -p %t.d
// RUN: cp %s %t.d/a.mlir && cp %S/Inputs/wpo-b.mlir %t.d/b.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n%t.d/b.mlir %t.b.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -wpo -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=COLD
// RUN: %backend -O2 -batch %t.jobs -wpo -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=WARM
// RUN: %clang %t.a.o %t.b.o -o %t.exe && %t.exe; test $? -eq 37
// Change a comment in a.mlir: a's entries miss, b's translation still hits.
// RUN: echo '// edit' >> %t.d/a.mlir
// RUN: %backend -O2 -batch %t.jobs -wpo -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=EDIT
// RUN: %clang %t.a.o %t.b.o -o %t.exe2 && %t.exe2; test $? -eq 37
// COLD: cache {{[0-9]+}} hits {{[1-9][0-9]*}} misses
// WARM: cache {{[0-9]+}} hits 0 misses
// EDIT: cache {{[1-9][0-9]*}} hits {{[1-9][0-9]*}} misses
module {
  llvm.func @"b.Small"(i64) -> i64
  llvm.func @"b.Count"() -> i64
  llvm.func @"b.UsesHelper"(i64) -> i64
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
// -prepare-only fills the cache without outputs; the real build then hits.
// RUN: rm -rf %t.cache2 %t.a.o %t.b.o
// RUN: %backend -O2 -batch %t.jobs -wpo -cache-dir %t.cache2 -prepare-only
// RUN: test ! -e %t.a.o
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache2 -prepare-only
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache2 -time 2>&1 | %FileCheck %s --check-prefix=DEVHIT
// RUN: %backend -O2 -batch %t.jobs -wpo -cache-dir %t.cache2 -time 2>&1 | %FileCheck %s --check-prefix=WPO
// DEVHIT: cache {{[0-9]+}} hits 0 misses
// WPO: cache {{[0-9]+}} hits {{[1-9][0-9]*}} misses
// A size limit evicts least recently used entries.
// RUN: rm -rf %t.cache3
// RUN: env MARMOT_CACHE_MAX_MB=1 %backend -O2 -batch %t.jobs -wpo -cache-dir %t.cache3
// RUN: test -f %t.cache3/.size
// RUN: dd if=/dev/zero of=%t.cache3/ff/big.o bs=1048576 count=2 2>/dev/null || (mkdir -p %t.cache3/ff && dd if=/dev/zero of=%t.cache3/ff/big.o bs=1048576 count=2 2>/dev/null)
// RUN: echo 3000000 > %t.cache3/.size
// RUN: env MARMOT_CACHE_MAX_MB=1 %backend -O2 -batch %t.jobs -wpo -cache-dir %t.cache3
// RUN: test ! -e %t.cache3/ff/big.o

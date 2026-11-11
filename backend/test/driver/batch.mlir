// RUN: rm -f %t.a.o %t.b.o
// RUN: printf '# comment\n%s %t.a.o\n\n%s %t.b.o\n' > %t.jobs
// RUN: %backend -batch %t.jobs -j 2 -time 2> %t.time
// RUN: %llvm-nm %t.a.o | %FileCheck %s
// RUN: %llvm-nm %t.b.o | %FileCheck %s
// RUN: %FileCheck %s --check-prefix=TIME < %t.time
// Batch mode fails as a whole when one job fails, and names the bad input.
// RUN: printf '%s %t.c.o\n%S/Inputs/bad.mlir %t.d.o\n' > %t.jobs2
// RUN: %not %backend -batch %t.jobs2 2>&1 | %FileCheck %s --check-prefix=ERR
// CHECK: T {{_?}}f
// TIME: sum of jobs{{ +}}parse
// TIME: 2 job(s)
// ERR: bad.mlir
module {
  llvm.func @f() {
    llvm.return
  }
}

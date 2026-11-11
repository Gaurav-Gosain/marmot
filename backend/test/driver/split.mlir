// -split-large: a module above N instructions is compiled as function groups
// (OUT, OUT.1.o, ...; listed in OUT.parts), linked together it runs as one.
// RUN: rm -rf %t.cache %t.d && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -split-large=8 -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=COLD
// RUN: test -f %t.a.o.parts
// RUN: %clang $(cat %t.a.o.parts) -o %t.exe && %t.exe; test $? -eq 42
// RUN: %backend -O2 -batch %t.jobs -split-large=8 -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=WARM
// RUN: %clang $(cat %t.a.o.parts) -o %t.exe2 && %t.exe2; test $? -eq 42
// Editing one function recompiles its group (and groups that inline it), not all.
// RUN: sed -i.bak 's/constant(5 : i64)/constant(6 : i64)/' %t.d/a.mlir
// RUN: %backend -O2 -batch %t.jobs -split-large=8 -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=EDIT
// RUN: %clang $(cat %t.a.o.parts) -o %t.exe3 && %t.exe3; test $? -eq 43
// Without splitting, no parts file is left behind.
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache
// RUN: test ! -e %t.a.o.parts
// COLD: compiled {{[2-9]}} of 1 objects, 1 modules split into {{[2-9]}} groups
// WARM: compiled 0 of 1 objects, 0 modules split
// EDIT: compiled {{[1-3]}} of 1 objects, 1 modules split into {{[4-9]}} groups
module {
  llvm.mlir.global private constant @msg("unused\00") {addr_space = 0 : i32}
  llvm.mlir.global internal @counter(1 : i64) : i64
  llvm.func internal @bump() -> i64 attributes {passthrough = ["noinline"]} {
    %p = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %p : !llvm.ptr -> i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %w = llvm.add %v, %one : i64
    llvm.store %w, %p : i64, !llvm.ptr
    llvm.return %w : i64
  }
  llvm.func @"p.five"() -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(5 : i64) : i64
    %b = llvm.call @bump() : () -> i64
    %r = llvm.add %c, %b : i64
    llvm.return %r : i64
  }
  llvm.func @"p.ten"() -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(10 : i64) : i64
    %d = llvm.mlir.constant(3 : i64) : i64
    %r = llvm.mul %c, %d : i64
    llvm.return %r : i64
  }
  llvm.func @"p.small"(%x: i64) -> i64 {
    %c = llvm.mlir.constant(2 : i64) : i64
    %r = llvm.add %x, %c : i64
    llvm.return %r : i64
  }
  llvm.func @main() -> i32 {
    %a = llvm.call @"p.five"() : () -> i64
    %b = llvm.call @"p.ten"() : () -> i64
    %s = llvm.add %a, %b : i64
    %t = llvm.call @"p.small"(%s) : (i64) -> i64
    %u = llvm.mlir.constant(3 : i64) : i64
    %v = llvm.add %t, %u : i64
    %r = llvm.trunc %v : i64 to i32
    llvm.return %r : i32
  }
}

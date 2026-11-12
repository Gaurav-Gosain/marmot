// -tier0: the batch compiles each module with tier0; a function it cannot
// compile goes to LLVM -O0 in OUT.1.o, listed with OUT in OUT.parts. Both
// objects come from the cache on the next build.
// UNSUPPORTED: x86_64, aarch64
// RUN: rm -rf %t.cache %t.d && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: %backend -O2 -tier0 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=COLD
// RUN: test -f %t.a.o.parts
// RUN: %clang $(cat %t.a.o.parts) -o %t.exe && %t.exe; test $? -eq 42
// RUN: rm -f %t.a.o %t.a.o.1.o %t.a.o.parts
// RUN: %backend -O2 -tier0 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=WARM
// RUN: %clang $(cat %t.a.o.parts) -o %t.exe2 && %t.exe2; test $? -eq 42
// Without the fallback function, one object and no parts file.
// RUN: sed -i.bak -e '/^  \/\/ BEGIN-ASM/,/^  \/\/ END-ASM/d' -e 's/llvm.call @asm_one() : () -> i64/llvm.mlir.constant(1 : i64) : i64/' %t.d/a.mlir
// RUN: %backend -O2 -tier0 -batch %t.jobs -cache-dir %t.cache -time 2>&1 | %FileCheck %s --check-prefix=ONE
// RUN: test ! -e %t.a.o.parts
// RUN: %clang %t.a.o -o %t.exe3 && %t.exe3; test $? -eq 42
// One job without a cache takes the tier0 path too.
// RUN: %backend -tier0 -batch %t.jobs -time 2>&1 | %FileCheck %s --check-prefix=NOCACHE
// RUN: %clang %t.a.o -o %t.exe4 && %t.exe4; test $? -eq 42
// NOCACHE: tier0: {{[1-9]}} functions, 0 fallbacks
// COLD: compiled 1 of 1 objects
// COLD: tier0: {{[1-9]}} functions, 1 fallbacks, 1 modules need LLVM
// WARM: compiled 0 of 1 objects
// ONE: tier0: {{[1-9]}} functions, 0 fallbacks, 0 modules need LLVM
module {
  llvm.mlir.global internal @counter(40 : i64) : i64
  llvm.func internal @bump() -> i64 {
    %p = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %p : !llvm.ptr -> i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %w = llvm.add %v, %one : i64
    llvm.store %w, %p : i64, !llvm.ptr
    llvm.return %w : i64
  }
  // BEGIN-ASM
  llvm.func internal @asm_one() -> i64 {
    %r = llvm.inline_asm "mov $0, #1", "=r" : () -> i64
    llvm.return %r : i64
  }
  // END-ASM
  llvm.func @main() -> i32 {
    %b = llvm.call @bump() : () -> i64
    %a = llvm.call @asm_one() : () -> i64
    %s = llvm.add %a, %b : i64
    %r = llvm.trunc %s : i64 to i32
    llvm.return %r : i32
  }
}

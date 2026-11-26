// Name hash (the default, MARMOT_SPLIT_NAMEHASH unset): a split module without an external definition is
// named by its package, the names it defines and its structural hash (code
// and constants), never by its cache key. Two modules that define the same names with other bodies (this
// file and Inputs/split_namehash_c.mlir, both only internal definitions) get
// different suffixes, so their groups link into one program.
// RUN: rm -rf %t && mkdir -p %t
// RUN: printf '%s %t/b.o\n%S/Inputs/split_namehash_c.mlir %t/c.o\n' > %t/jobs
// RUN: env -u MARMOT_SPLIT_NAMEHASH %backend -O2 -batch %t/jobs -split-large=8
// RUN: test -f %t/b.o.parts && test -f %t/c.o.parts
// RUN: printf 'int main(void) { return 0; }\n' > %t/main.c
// RUN: %clang $(cat %t/b.o.parts) $(cat %t/c.o.parts) %t/main.c -o %t/exe
// RUN: %t/exe
// Two packages whose modules define the same names with the same code.
// RUN: printf '%S/Inputs/split_namehash_p.mlir %t/p.o\n%S/Inputs/split_namehash_q.mlir %t/q.o\n' > %t/jobs2
// RUN: env -u MARMOT_SPLIT_NAMEHASH %backend -O2 -batch %t/jobs2 -split-large=8
// RUN: test -f %t/p.o.parts && test -f %t/q.o.parts
// RUN: %clang $(cat %t/p.o.parts) $(cat %t/q.o.parts) %t/main.c -o %t/exe2
// RUN: %t/exe2
module {
  llvm.mlir.global internal @counter(1 : i64) : i64
  llvm.mlir.global_ctors ctors = [@init], priorities = [65535 : i32], data = [#llvm.zero]
  llvm.func internal @bump() -> i64 attributes {passthrough = ["noinline"]} {
    %p = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %p : !llvm.ptr -> i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %w = llvm.add %v, %one : i64
    llvm.store %w, %p : i64, !llvm.ptr
    llvm.return %w : i64
  }
  llvm.func internal @twice() -> i64 attributes {passthrough = ["noinline"]} {
    %a = llvm.call @bump() : () -> i64
    %b = llvm.call @bump() : () -> i64
    %c = llvm.mlir.constant(3 : i64) : i64
    %s = llvm.add %a, %b : i64
    %r = llvm.mul %s, %c : i64
    llvm.return %r : i64
  }
  llvm.func internal @init() attributes {passthrough = ["noinline"]} {
    %r = llvm.call @twice() : () -> i64
    %p = llvm.mlir.addressof @counter : !llvm.ptr
    llvm.store %r, %p : i64, !llvm.ptr
    llvm.return
  }
}

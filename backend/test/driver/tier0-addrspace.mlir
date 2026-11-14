// tier0 reads pointers in an address space (!llvm.ptr<1>, GC pointers) and
// llvm.addrspacecast, and compiles them like address space 0 pointers; the
// fast reader agrees with the MLIR parser.
// RUN: rm -rf %t.d && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: MARMOT_PARSE_CHECK=1 %backend -tier0 -batch %t.jobs -time 2>&1 | %FileCheck %s
// RUN: %clang %t.a.o -o %t.exe && %t.exe; test $? -eq 42
// CHECK: tier0: {{[1-9]}} functions, 0 fallbacks
module {
  llvm.func @load1(%p: !llvm.ptr<1>) -> i64 {
    %v = llvm.load %p : !llvm.ptr<1> -> i64
    llvm.return %v : i64
  }
  llvm.func @main() -> i32 {
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c40 = llvm.mlir.constant(40 : i64) : i64
    %a = llvm.alloca %c1 x i64 : (i64) -> !llvm.ptr
    llvm.store %c40, %a : i64, !llvm.ptr
    %g = llvm.addrspacecast %a : !llvm.ptr to !llvm.ptr<1>
    %v = llvm.call @load1(%g) : (!llvm.ptr<1>) -> i64
    %q = llvm.getelementptr %g[1] : (!llvm.ptr<1>) -> !llvm.ptr<1>, i8
    %b = llvm.addrspacecast %q : !llvm.ptr<1> to !llvm.ptr
    %one = llvm.mlir.constant(2 : i8) : i8
    llvm.store %one, %b : i8, !llvm.ptr
    %w = llvm.load %a : !llvm.ptr -> i64
    %s = llvm.sub %w, %c40 : i64
    %d = llvm.lshr %s, %c1 : i64
    %r = llvm.add %v, %d : i64
    %t = llvm.mlir.constant(256 : i64) : i64
    %u = llvm.udiv %d, %t : i64
    %x = llvm.add %r, %u : i64
    %y = llvm.sub %x, %d : i64
    %z = llvm.add %y, %c1 : i64
    %r32 = llvm.trunc %z : i64 to i32
    llvm.return %r32 : i32
  }
}

// A struct GEP on a global is folded to a byte offset while translating; it
// must use the target's data layout (i64 aligned to 8), not LLVM's default
// (i64 aligned to 4), or reads miss the initializer by 4 bytes
// (.work/static_layout_repro.go, 2026-11-12).
// RUN: %backend -O0 -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -O0 -o %t.o %s && %clang %t.o -o %t.exe && %t.exe; test $? -eq 2
// RUN: %backend -O2 -o %t2.o %s && %clang %t2.o -o %t2.exe && %t2.exe; test $? -eq 2
// RUN: %backend -target linux/amd64 -O0 -emit=llvm -o - %s | %FileCheck %s
// CHECK: load i64, ptr getelementptr inbounds nuw (i8, ptr @g, i64 8)
module {
  llvm.mlir.global external @g() : !llvm.struct<(i8, i64)> {
    %u = llvm.mlir.undef : !llvm.struct<(i8, i64)>
    %a = llvm.mlir.constant(1 : i8) : i8
    %b = llvm.mlir.constant(2 : i64) : i64
    %s0 = llvm.insertvalue %a, %u[0] : !llvm.struct<(i8, i64)>
    %s1 = llvm.insertvalue %b, %s0[1] : !llvm.struct<(i8, i64)>
    llvm.return %s1 : !llvm.struct<(i8, i64)>
  }
  llvm.func @main() -> i32 {
    %p = llvm.mlir.addressof @g : !llvm.ptr
    %q = llvm.getelementptr inbounds %p[0, 1] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(i8, i64)>
    %v = llvm.load %q : !llvm.ptr -> i64
    %r = llvm.trunc %v : i64 to i32
    llvm.return %r : i32
  }
}

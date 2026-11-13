// A global with a go.init string gets the initializer region the string
// describes, at -O0 and with tier0; a malformed string is an error.
// RUN: %backend -O0 -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -O0 -o %t.o %s && %clang %t.o -o %t.exe && %t.exe; test $? -eq 42
// RUN: %backend -tier0 -o %t0.o %s && %clang %t0.o -o %t0.exe && %t0.exe; test $? -eq 42
// A long integer array (an embedded table) is one dense constant, not a
// chain of insertvalues that LLVM translates in quadratic time.
// RUN: python3 %S/Inputs/big_init.py %t.big.mlir
// RUN: timeout 60 %backend -O2 -emit=llvm -o - %t.big.mlir | %FileCheck %s --check-prefix=BIG
// BIG: @t = {{.*}}global [100000 x i8] c"\00\01\02
// RUN: sed 's/i40;/i40/' %s > %t.bad.mlir
// RUN: %not %backend -O0 -o %t.bad.o %t.bad.mlir 2>&1 | %FileCheck %s --check-prefix=BAD
// CHECK: @g = linkonce_odr constant { i64, i8, ptr, double, [2 x i32], { ptr, i64 } } { i64 40, i8 -2, ptr @x, double 1.500000e+00, [2 x i32] [i32 1, i32 2], { ptr, i64 } zeroinitializer }
// BAD: global g: bad go.init at offset
module {
  llvm.mlir.global internal @x(2 : i64) {addr_space = 0 : i32} : i64
  llvm.mlir.global linkonce_odr constant @g() {addr_space = 0 : i32, go.init = "(i40;i-2;a1:xf3FF8000000000000(i1;i2;)z)"} : !llvm.struct<(i64, i8, ptr, f64, array<2 x i32>, struct<(ptr, i64)>)>
  llvm.func @main() -> i32 {
    %g = llvm.mlir.addressof @g : !llvm.ptr
    %a = llvm.load %g : !llvm.ptr -> i64
    %pb = llvm.getelementptr %g[0, 1] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(i64, i8, ptr, f64, array<2 x i32>, struct<(ptr, i64)>)>
    %b8 = llvm.load %pb : !llvm.ptr -> i8
    %b = llvm.sext %b8 : i8 to i64
    %pp = llvm.getelementptr %g[0, 2] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(i64, i8, ptr, f64, array<2 x i32>, struct<(ptr, i64)>)>
    %p = llvm.load %pp : !llvm.ptr -> !llvm.ptr
    %c = llvm.load %p : !llvm.ptr -> i64
    %pd = llvm.getelementptr %g[0, 4, 1] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(i64, i8, ptr, f64, array<2 x i32>, struct<(ptr, i64)>)>
    %d32 = llvm.load %pd : !llvm.ptr -> i32
    %d = llvm.sext %d32 : i32 to i64
    %s1 = llvm.add %a, %b : i64
    %s2 = llvm.add %s1, %c : i64
    %s3 = llvm.add %s2, %d : i64
    %r = llvm.trunc %s3 : i64 to i32
    llvm.return %r : i32
  }
}

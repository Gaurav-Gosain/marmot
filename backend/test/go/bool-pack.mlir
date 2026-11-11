// RUN: %marmot-opt --go-bool-pack %s | %FileCheck %s
!slice = !llvm.struct<(ptr, i64, i64)>
module {
  llvm.mlir.global external constant @"type:bool"() : !llvm.array<56 x i8>
  // A local []bool used only through indexing becomes a bit array; the
  // bounds check stays against the original length.
  // CHECK-LABEL: llvm.func @sieve
  // CHECK: %[[B:.*]] = llvm.select %{{.*}}, %arg0, %{{.*}} : i1, i64
  // CHECK: go.make_slice @"type:bool", i8(%[[B]], %[[B2:.*]]) : i64
  // CHECK: go.bounds_check %arg1, %arg0 : i64
  // CHECK: llvm.lshr %arg1
  // CHECK: llvm.or
  // CHECK: go.bounds_check %arg1, %arg0 : i64
  // CHECK: llvm.load {{.*}} : !llvm.ptr -> i8
  // CHECK-NOT: go.index
  llvm.func @sieve(%n: i64, %i: i64) -> i8 {
    %s = go.make_slice @"type:bool", i8(%n, %n) : i64
    %p = llvm.extractvalue %s[0] : !slice
    %l = llvm.extractvalue %s[1] : !slice
    %a = go.index %p[%i] len %l, i8 : i64
    %t = llvm.mlir.constant(1 : i8) : i8
    llvm.store %t, %a : i8, !llvm.ptr
    %p2 = llvm.extractvalue %s[0] : !slice
    %b = go.index %p2[%i] len %l, i8 : i64
    %v = llvm.load %b : !llvm.ptr -> i8
    llvm.return %v : i8
  }
  // Passed to a call: left alone.
  // CHECK-LABEL: llvm.func @escapes
  // CHECK: go.make_slice @"type:bool", i8(%arg0, %arg0) : i64
  // CHECK: go.index
  llvm.func @use(!slice)
  llvm.func @escapes(%n: i64, %i: i64) {
    %s = go.make_slice @"type:bool", i8(%n, %n) : i64
    llvm.call @use(%s) : (!slice) -> ()
    %p = llvm.extractvalue %s[0] : !slice
    %a = go.index %p[%i] len %n, i8 : i64
    %t = llvm.mlir.constant(1 : i8) : i8
    llvm.store %t, %a : i8, !llvm.ptr
    llvm.return
  }
}

// RUN: %marmot-opt --aggregate-copies --split-input-file --verify-diagnostics %s | %FileCheck %s
!big = !llvm.struct<(i64, array<1024 x i8>)>
!huge = !llvm.array<2000000 x i8>

// Load then store, with only an allocation call between: memmove.
// CHECK-LABEL: llvm.func @copy
// CHECK: %[[D:.*]] = llvm.call @runtime.newobject
// CHECK: %[[N:.*]] = llvm.mlir.constant(1032 : i64) : i64
// CHECK: "llvm.intr.memmove"(%[[D]], %arg0, %[[N]]) <{isVolatile = false}>
// CHECK-NOT: llvm.load
llvm.func @runtime.newobject(!llvm.ptr) -> !llvm.ptr
llvm.func @copy(%src: !llvm.ptr, %t: !llvm.ptr) -> !llvm.ptr {
  %v = llvm.load %src : !llvm.ptr -> !big
  %d = llvm.call @runtime.newobject(%t) : (!llvm.ptr) -> !llvm.ptr
  llvm.store %v, %d : !big, !llvm.ptr
  llvm.return %d : !llvm.ptr
}

// -----

!big = !llvm.struct<(i64, array<1024 x i8>)>
// Element reads become element loads at the original load.
// CHECK-LABEL: llvm.func @field
// CHECK: %[[A:.*]] = llvm.getelementptr %arg0[0, 0] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(i64, array<1024 x i8>)>
// CHECK: %[[V:.*]] = llvm.load %[[A]] : !llvm.ptr -> i64
// CHECK: llvm.return %[[V]]
llvm.func @field(%src: !llvm.ptr) -> i64 {
  %v = llvm.load %src : !llvm.ptr -> !big
  %x = llvm.extractvalue %v[0] : !big
  llvm.return %x : i64
}

// -----

!big = !llvm.struct<(i64, array<1024 x i8>)>
// A store of zero becomes a memset.
// CHECK-LABEL: llvm.func @zero
// CHECK: "llvm.intr.memset"(%arg0, %{{.*}}, %{{.*}}) <{isVolatile = false}>
llvm.func @zero(%p: !llvm.ptr) {
  %z = llvm.mlir.zero : !big
  llvm.store %z, %p : !big, !llvm.ptr
  llvm.return
}

// -----

!big = !llvm.struct<(i64, array<1024 x i8>)>
// A write between the load and the store: left alone (it is under the
// size limit, so no error).
// CHECK-LABEL: llvm.func @clobber
// CHECK: llvm.load %arg0
// CHECK: llvm.store
// CHECK: llvm.store %{{.*}}, %arg1 : !llvm.struct
llvm.func @clobber(%src: !llvm.ptr, %dst: !llvm.ptr) {
  %v = llvm.load %src : !llvm.ptr -> !big
  %c = llvm.mlir.constant(1 : i64) : i64
  llvm.store %c, %src : i64, !llvm.ptr
  llvm.store %v, %dst : !big, !llvm.ptr
  llvm.return
}

// -----

!huge = !llvm.array<2000000 x i8>
llvm.func @bad(%src: !llvm.ptr, %dst: !llvm.ptr) {
  // expected-error @+1 {{aggregate value of 2000000 bytes}}
  %v = llvm.load %src : !llvm.ptr -> !huge
  %c = llvm.mlir.constant(1 : i64) : i64
  llvm.store %c, %src : i64, !llvm.ptr
  llvm.store %v, %dst : !huge, !llvm.ptr
  llvm.return
}

// -----

// A large zero-initialized global is constant data, not an SSA value in code.
// CHECK-LABEL: llvm.mlir.global external @scratch
llvm.mlir.global external @scratch() : !llvm.array<33554432 x i8> {
  %0 = llvm.mlir.zero : !llvm.array<33554432 x i8>
  llvm.return %0 : !llvm.array<33554432 x i8>
}

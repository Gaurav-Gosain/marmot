// MARMOT_BOXSCALAR=1: heap boxes of small scalars allocate nothing, as gc:
// 1-byte values point into runtime.staticuint64s, 2, 4 and 8-byte integers
// and floats go through runtime.convT16/32/64. Other values keep newobject,
// and without the switch every box does.
// RUN: env MARMOT_BOXSCALAR=1 %marmot-opt --go-to-llvm %s | %FileCheck %s
// RUN: %marmot-opt --go-to-llvm %s | %FileCheck %s --check-prefix=OFF
module {
  llvm.mlir.global external constant @"type:int"() : !llvm.array<56 x i8>
  llvm.func @use(!llvm.ptr)
  // CHECK-LABEL: llvm.func @boxes
  // CHECK: llvm.call @runtime.convT64(%arg0)
  // CHECK: %[[I:.*]] = llvm.zext %arg1 : i8 to i64
  // CHECK: llvm.getelementptr %{{.*}}[%[[I]]] : (!llvm.ptr, i64) -> !llvm.ptr, i64
  // CHECK: llvm.zext %arg2 : i1 to i64
  // CHECK: %[[F:.*]] = llvm.bitcast %arg3 : f64 to i64
  // CHECK: llvm.call @runtime.convT64(%[[F]])
  // CHECK: llvm.call @runtime.convT16(%arg4)
  // CHECK: llvm.call @runtime.newobject
  // CHECK-NOT: llvm.call @runtime.newobject
  // CHECK: llvm.mlir.global external @runtime.staticuint64s() {{.*}} : !llvm.array<256 x i64>
  // OFF-LABEL: llvm.func @boxes
  // OFF-COUNT-6: llvm.call @runtime.newobject
  // OFF-NOT: convT
  llvm.func @boxes(%a: i64, %b: i8, %c: i1, %d: f64, %e: i16, %s: !llvm.struct<(i64, i64)>) {
    %0 = go.box @"type:int", %a : i64
    llvm.call @use(%0) : (!llvm.ptr) -> ()
    %1 = go.box @"type:int", %b : i8
    llvm.call @use(%1) : (!llvm.ptr) -> ()
    %2 = go.box @"type:int", %c : i1
    llvm.call @use(%2) : (!llvm.ptr) -> ()
    %3 = go.box @"type:int", %d : f64
    llvm.call @use(%3) : (!llvm.ptr) -> ()
    %4 = go.box @"type:int", %e : i16
    llvm.call @use(%4) : (!llvm.ptr) -> ()
    %5 = go.box @"type:int", %s : !llvm.struct<(i64, i64)>
    llvm.call @use(%5) : (!llvm.ptr) -> ()
    llvm.return
  }
}

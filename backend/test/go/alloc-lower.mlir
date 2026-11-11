// RUN: %marmot-opt --go-to-llvm %s | %FileCheck %s
// RUN: %marmot-opt %s | %marmot-opt | %FileCheck %s --check-prefix=ROUND
// RUN: %backend -O0 -emit=llvm -o - %s | %FileCheck %s --check-prefix=IR

module {
  llvm.mlir.global linkonce_odr constant @"type:int"() : !llvm.array<56 x i8>
  llvm.mlir.global linkonce_odr constant @"type:main.T"() : !llvm.array<56 x i8>
  llvm.mlir.global linkonce_odr constant @"type:closure.main.f$1"() : !llvm.array<56 x i8>
  llvm.func @"main.f$1"(!llvm.ptr) -> i64

  // ROUND-LABEL: llvm.func @new
  // ROUND: go.new @"type:main.T", !llvm.struct<(i64, ptr)>
  // CHECK-LABEL: llvm.func @new
  // CHECK: %[[T:.*]] = llvm.mlir.addressof @"type:main.T" : !llvm.ptr
  // CHECK: %[[P:.*]] = llvm.call @runtime.newobject(%[[T]]) : (!llvm.ptr) -> !llvm.ptr
  // CHECK: llvm.return %[[P]]
  llvm.func @new() -> !llvm.ptr {
    %p = go.new @"type:main.T", !llvm.struct<(i64, ptr)>
    llvm.return %p : !llvm.ptr
  }

  // ROUND: go.make_slice @"type:int", i64(%{{.*}}, %{{.*}}) : i64
  // CHECK-LABEL: llvm.func @mkslice
  // CHECK: %[[T:.*]] = llvm.mlir.addressof @"type:int"
  // CHECK: %[[P:.*]] = llvm.call @runtime.makeslice(%[[T]], %arg0, %arg1) : (!llvm.ptr, i64, i64) -> !llvm.ptr
  // CHECK: %[[S0:.*]] = llvm.mlir.poison : !llvm.struct<(ptr, i64, i64)>
  // CHECK: %[[S1:.*]] = llvm.insertvalue %[[P]], %[[S0]][0]
  // CHECK: %[[S2:.*]] = llvm.insertvalue %arg0, %[[S1]][1]
  // CHECK: %[[S3:.*]] = llvm.insertvalue %arg1, %[[S2]][2]
  // CHECK: llvm.return %[[S3]]
  llvm.func @mkslice(%n: i64, %c: i64) -> !llvm.struct<(ptr, i64, i64)> {
    %s = go.make_slice @"type:int", i64(%n, %c) : i64
    llvm.return %s : !llvm.struct<(ptr, i64, i64)>
  }

  // ROUND: go.make_closure @{{"?}}main.f$1{{"?}}(%{{.*}}, %{{.*}}), @"type:closure.main.f$1" : (i64, i1) -> !llvm.ptr
  // CHECK-LABEL: llvm.func @closure
  // CHECK: %[[P:.*]] = llvm.call @runtime.newobject
  // CHECK: %[[F:.*]] = llvm.mlir.addressof @{{"?}}main.f$1{{"?}}
  // CHECK: llvm.store %[[F]], %[[P]]
  // CHECK: %[[A1:.*]] = llvm.getelementptr %[[P]][0, 1] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(ptr, i64, i8)>
  // CHECK: llvm.store %arg0, %[[A1]] : i64, !llvm.ptr
  // CHECK: %[[A2:.*]] = llvm.getelementptr %[[P]][0, 2]
  // CHECK: %[[B:.*]] = llvm.zext %arg1 : i1 to i8
  // CHECK: llvm.store %[[B]], %[[A2]] : i8, !llvm.ptr
  llvm.func @closure(%x: i64, %b: i1) -> !llvm.ptr {
    %f = go.make_closure @"main.f$1"(%x, %b), @"type:closure.main.f$1" : (i64, i1) -> !llvm.ptr
    llvm.return %f : !llvm.ptr
  }

  // ROUND: go.box @"type:int", %{{.*}} : i64
  // CHECK-LABEL: llvm.func @box
  // CHECK: %[[P:.*]] = llvm.call @runtime.newobject
  // CHECK: llvm.store %arg0, %[[P]] : i64, !llvm.ptr
  llvm.func @box(%x: i64) -> !llvm.ptr {
    %p = go.box @"type:int", %x : i64
    llvm.return %p : !llvm.ptr
  }
  // CHECK: llvm.func @runtime.newobject(!llvm.ptr) -> (!llvm.ptr {llvm.noalias})
  // CHECK: llvm.func @runtime.makeslice(!llvm.ptr, i64, i64) -> (!llvm.ptr {llvm.noalias})
  // IR: declare noalias ptr @runtime.newobject(ptr)
}

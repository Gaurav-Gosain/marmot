// RUN: %marmot-opt --go-to-llvm %s | %FileCheck %s
// RUN: %marmot-opt %s | %marmot-opt | %FileCheck %s --check-prefix=ROUND

module {
  llvm.mlir.global external constant @"type:any"() : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:main.I"() : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:int"() : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"itab:main.T,main.I"() : !llvm.array<24 x i8>

  // ROUND-LABEL: llvm.func @mk
  // ROUND: go.make_iface @"itab:main.T,main.I", %arg0
  // CHECK-LABEL: llvm.func @mk
  // CHECK: %[[T:.*]] = llvm.mlir.addressof @"itab:main.T,main.I"
  // CHECK: %[[S0:.*]] = llvm.mlir.poison : !llvm.struct<(ptr, ptr)>
  // CHECK: %[[S1:.*]] = llvm.insertvalue %[[T]], %[[S0]][0]
  // CHECK: llvm.insertvalue %arg0, %[[S1]][1]
  llvm.func @mk(%p: !llvm.ptr) -> !llvm.struct<(ptr, ptr)> {
    %i = go.make_iface @"itab:main.T,main.I", %p
    llvm.return %i : !llvm.struct<(ptr, ptr)>
  }

  // ROUND-LABEL: llvm.func @call
  // ROUND: go.iface_call %arg0[3](%arg1) : (i64) -> i32
  // CHECK-LABEL: llvm.func @call
  // CHECK: %[[TAB:.*]] = llvm.extractvalue %arg0[0]
  // CHECK: %[[DATA:.*]] = llvm.extractvalue %arg0[1]
  // CHECK: llvm.icmp "ne" %[[TAB]]
  // CHECK: llvm.cond_br
  // CHECK: %[[SLOT:.*]] = llvm.getelementptr %[[TAB]][0, 4, 3] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(ptr, ptr, i32, i32, array<0 x ptr>)>
  // CHECK: %[[FN:.*]] = llvm.load %[[SLOT]] invariant : !llvm.ptr -> !llvm.ptr
  // CHECK: llvm.call %[[FN]](%[[DATA]], %arg1) : !llvm.ptr, (!llvm.ptr, i64) -> i32
  llvm.func @call(%i: !llvm.struct<(ptr, ptr)>, %x: i64) -> i32 {
    %r = go.iface_call %i[3](%x) : (i64) -> i32
    llvm.return %r : i32
  }

  // ROUND-LABEL: llvm.func @concrete
  // ROUND: go.type_assert %arg0, @"type:any" -> @"type:int"{{$}}
  // CHECK-LABEL: llvm.func @concrete
  // CHECK: %[[WANT:.*]] = llvm.mlir.addressof @"type:int"
  // CHECK: %[[SRC:.*]] = llvm.mlir.addressof @"type:any"
  // CHECK: %[[DATA:.*]] = llvm.extractvalue %arg0[1]
  // CHECK: %[[HAVE:.*]] = llvm.extractvalue %arg0[0]
  // CHECK: %[[OK:.*]] = llvm.icmp "eq" %[[HAVE]], %[[WANT]]
  // CHECK: llvm.cond_br %[[OK]] weights([1048576, 1]), ^{{.*}}, ^{{.*}}(%[[HAVE]], %[[WANT]], %[[SRC]] : !llvm.ptr, !llvm.ptr, !llvm.ptr)
  // CHECK: llvm.return %[[DATA]]
  // CHECK: llvm.call @runtime.panicTypeAssert
  llvm.func @concrete(%e: !llvm.struct<(ptr, ptr)>) -> !llvm.ptr {
    %d = go.type_assert %e, @"type:any" -> @"type:int"
    llvm.return %d : !llvm.ptr
  }

  // ROUND-LABEL: llvm.func @from_iface_ok
  // ROUND: go.type_assert %arg0, @"type:main.I" -> @"type:int" from_iface commaok
  // CHECK-LABEL: llvm.func @from_iface_ok
  // CHECK: %[[TAB:.*]] = llvm.extractvalue %arg0[0]
  // CHECK: llvm.cond_br %{{.*}}, ^[[LOAD:.*]], ^[[CONT:.*]](%{{.*}} : !llvm.ptr)
  // CHECK: ^[[LOAD]]:
  // CHECK: %[[S:.*]] = llvm.getelementptr %[[TAB]][1]
  // CHECK: %[[T:.*]] = llvm.load %[[S]] invariant
  // CHECK: llvm.br ^[[CONT]](%[[T]] : !llvm.ptr)
  // CHECK: ^[[CONT]](%[[HAVE:.*]]: !llvm.ptr):
  // CHECK: %[[OK:.*]] = llvm.icmp "eq" %[[HAVE]]
  // CHECK: llvm.select %[[OK]]
  llvm.func @from_iface_ok(%i: !llvm.struct<(ptr, ptr)>) -> i1 {
    %d, %ok = go.type_assert %i, @"type:main.I" -> @"type:int" from_iface commaok
    llvm.return %ok : i1
  }

  // ROUND-LABEL: llvm.func @to_iface
  // ROUND: go.type_assert %arg0, @"type:any" -> @"type:main.I" to_iface{{$}}
  // CHECK-LABEL: llvm.func @to_iface
  // CHECK: llvm.cond_br
  // CHECK: llvm.call @runtime.assertE2I(
  llvm.func @to_iface(%e: !llvm.struct<(ptr, ptr)>) -> !llvm.struct<(ptr, ptr)> {
    %i = go.type_assert %e, @"type:any" -> @"type:main.I" to_iface
    llvm.return %i : !llvm.struct<(ptr, ptr)>
  }

  // CHECK-LABEL: llvm.func @to_iface_ok
  // CHECK: llvm.call @runtime.assertE2I2(
  llvm.func @to_iface_ok(%e: !llvm.struct<(ptr, ptr)>) -> i1 {
    %i, %ok = go.type_assert %e, @"type:any" -> @"type:main.I" to_iface commaok
    llvm.return %ok : i1
  }

  // CHECK-LABEL: llvm.func @to_any
  // CHECK: llvm.icmp "ne"
  // CHECK-NOT: llvm.call @runtime.assert
  // CHECK: llvm.return
  llvm.func @to_any(%i: !llvm.struct<(ptr, ptr)>) -> !llvm.struct<(ptr, ptr)> {
    %e, %ok = go.type_assert %i, @"type:main.I" -> @"type:any" from_iface to_any commaok
    llvm.return %e : !llvm.struct<(ptr, ptr)>
  }
}

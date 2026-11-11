// RUN: %marmot-opt --go-to-llvm %s | %FileCheck %s
// RUN: %marmot-opt %s | %marmot-opt | %FileCheck %s --check-prefix=ROUND

module {
  // ROUND-LABEL: llvm.func @idx
  // ROUND: go.index %arg0[%arg1] len %arg2, i32 : i64
  // ROUND: go.bounds_check %arg3, %arg2 unsigned : i64
  // CHECK-LABEL: llvm.func @idx
  // CHECK: %[[OK:.*]] = llvm.icmp "ult" %arg1, %arg2 : i64
  // CHECK: %[[C:.*]] = llvm.mlir.constant(0 : i64) : i64
  // CHECK: %[[S:.*]] = llvm.mlir.constant(true) : i1
  // CHECK: llvm.cond_br %[[OK]] weights([1048576, 1]), ^[[CONT:.*]], ^[[PANIC:.*]](%[[C]], %arg1, %[[S]], %arg2 : i64, i64, i1, i64)
  // CHECK: ^[[CONT]]:
  // CHECK: %[[A:.*]] = llvm.getelementptr %arg0[%arg1] : (!llvm.ptr, i64) -> !llvm.ptr, i32
  // CHECK: llvm.load %[[A]]
  // All bounds checks in a function share one panic block.
  // CHECK: %[[U:.*]] = llvm.mlir.constant(false) : i1
  // CHECK: llvm.cond_br %{{.*}}, ^{{.*}}, ^[[PANIC]](%{{.*}}, %arg3, %[[U]], %arg2 : i64, i64, i1, i64)
  // CHECK: ^[[PANIC]](%[[PC:.*]]: i64, %[[PX:.*]]: i64, %[[PS:.*]]: i1, %[[PY:.*]]: i64):
  // CHECK: llvm.call @runtime.panicBounds(%[[PC]], %[[PX]], %[[PS]], %[[PY]]) : (i64, i64, i1, i64) -> ()
  // CHECK: llvm.unreachable
  llvm.func @idx(%p: !llvm.ptr, %i: i64, %n: i64, %j: i64) -> i32 {
    %a = go.index %p[%i] len %n, i32 : i64
    %v = llvm.load %a : !llvm.ptr -> i32
    go.bounds_check %j, %n unsigned : i64
    llvm.return %v : i32
  }

  // A narrower index type is extended for the panic call.
  // CHECK-LABEL: llvm.func @idx32
  // CHECK: llvm.icmp "ult" %arg1, %arg2 : i32
  // CHECK: llvm.sext %arg1 : i32 to i64
  // CHECK: llvm.sext %arg2 : i32 to i64
  llvm.func @idx32(%p: !llvm.ptr, %i: i32, %n: i32) {
    go.bounds_check %i, %n : i32
    llvm.return
  }

  // ROUND-LABEL: llvm.func @nil
  // ROUND: go.nil_check %arg0
  // CHECK-LABEL: llvm.func @nil
  // CHECK: %[[Z:.*]] = llvm.mlir.zero : !llvm.ptr
  // CHECK: %[[OK:.*]] = llvm.icmp "ne" %arg0, %[[Z]] : !llvm.ptr
  // CHECK: llvm.cond_br %[[OK]] weights([1048576, 1]), ^{{.*}}, ^[[P:.*]]
  // CHECK: ^[[P]]:
  // CHECK: llvm.call @runtime.panicnil() : () -> ()
  llvm.func @nil(%p: !llvm.ptr) -> i64 {
    go.nil_check %p
    %v = llvm.load %p : !llvm.ptr -> i64
    llvm.return %v : i64
  }

  // Three index form: max <= cap (code 5), hi <= max (6), lo <= hi (7).
  // ROUND-LABEL: llvm.func @slice3
  // ROUND: go.slice %arg0[%arg1 : %arg2 : %arg3] cap %arg4, i64 : i64 -> !llvm.struct<(ptr, i64, i64)>
  // CHECK-LABEL: llvm.func @slice3
  // CHECK: llvm.icmp "ule" %arg3, %arg4
  // CHECK: llvm.mlir.constant(5 : i64)
  // CHECK: llvm.icmp "ule" %arg2, %arg3
  // CHECK: llvm.mlir.constant(6 : i64)
  // CHECK: llvm.icmp "ule" %arg1, %arg2
  // CHECK: llvm.mlir.constant(7 : i64)
  // CHECK: %[[LEN:.*]] = llvm.sub %arg2, %arg1
  // CHECK: %[[CAP:.*]] = llvm.sub %arg3, %arg1
  // CHECK: %[[MOVED:.*]] = llvm.getelementptr %arg0[%arg1] : (!llvm.ptr, i64) -> !llvm.ptr, i64
  // CHECK: %[[EMPTY:.*]] = llvm.icmp "eq" %[[CAP]]
  // CHECK: llvm.select %[[EMPTY]], %arg0, %[[MOVED]]
  llvm.func @slice3(%p: !llvm.ptr, %lo: i64, %hi: i64, %max: i64, %cap: i64) -> !llvm.struct<(ptr, i64, i64)> {
    %s = go.slice %p[%lo : %hi : %max] cap %cap, i64 : i64 -> !llvm.struct<(ptr, i64, i64)>
    llvm.return %s : !llvm.struct<(ptr, i64, i64)>
  }

  // Two index slice: hi <= cap (code 2), lo <= hi (3); new cap is cap - lo.
  // CHECK-LABEL: llvm.func @slice2
  // CHECK: llvm.icmp "ule" %arg2, %arg3
  // CHECK: llvm.mlir.constant(2 : i64)
  // CHECK: llvm.mlir.constant(3 : i64)
  // CHECK: llvm.sub %arg2, %arg1
  // CHECK: llvm.sub %arg3, %arg1
  llvm.func @slice2(%p: !llvm.ptr, %lo: i64, %hi: i64, %cap: i64) -> !llvm.struct<(ptr, i64, i64)> {
    %s = go.slice %p[%lo : %hi] cap %cap, i64 : i64 -> !llvm.struct<(ptr, i64, i64)>
    llvm.return %s : !llvm.struct<(ptr, i64, i64)>
  }

  // An array bound reports length (code 1).
  // ROUND-LABEL: llvm.func @slicearr
  // ROUND: go.slice %arg0[%arg1 : %arg2] cap %arg3, i64 len_bound : i64
  // CHECK-LABEL: llvm.func @slicearr
  // CHECK: llvm.mlir.constant(1 : i64)
  llvm.func @slicearr(%p: !llvm.ptr, %lo: i64, %hi: i64, %n: i64) -> !llvm.struct<(ptr, i64, i64)> {
    %s = go.slice %p[%lo : %hi] cap %n, i64 len_bound : i64 -> !llvm.struct<(ptr, i64, i64)>
    llvm.return %s : !llvm.struct<(ptr, i64, i64)>
  }

  // ROUND-LABEL: llvm.func @substr
  // ROUND: go.slice %arg0[%arg1 : %arg2] cap %arg3, i8 : i64 -> !llvm.struct<(ptr, i64)>
  // CHECK-LABEL: llvm.func @substr
  // CHECK: llvm.mlir.constant(1 : i64)
  // CHECK: llvm.mlir.constant(3 : i64)
  // CHECK: %[[LEN:.*]] = llvm.sub %arg2, %arg1
  // CHECK: llvm.icmp "eq" %[[LEN]]
  // CHECK-NOT: [2]
  // CHECK: llvm.return
  llvm.func @substr(%p: !llvm.ptr, %lo: i64, %hi: i64, %n: i64) -> !llvm.struct<(ptr, i64)> {
    %s = go.slice %p[%lo : %hi] cap %n, i8 : i64 -> !llvm.struct<(ptr, i64)>
    llvm.return %s : !llvm.struct<(ptr, i64)>
  }
  // CHECK: llvm.func @runtime.panicBounds(i64, i64, i1, i64) attributes {passthrough = ["noreturn", "cold", "nounwind"]}
}

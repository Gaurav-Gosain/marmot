// RUN: %marmot-opt --go-to-llvm %s | %FileCheck %s
// RUN: %marmot-opt %s | %marmot-opt | %FileCheck %s --check-prefix=ROUND

module {
  // ROUND-LABEL: llvm.func @idx
  // ROUND: go.index %arg0[%arg1] len %arg2, i32 : i64
  // CHECK-LABEL: llvm.func @idx
  // CHECK: %[[OK:.*]] = llvm.icmp "ult" %arg1, %arg2 : i64
  // CHECK: llvm.cond_br %[[OK]] weights([1048576, 1]), ^[[CONT:.*]], ^[[PANIC:.*]](%arg1, %arg2 : i64, i64)
  // CHECK: ^[[CONT]]:
  // CHECK: %[[A:.*]] = llvm.getelementptr %arg0[%arg1] : (!llvm.ptr, i64) -> !llvm.ptr, i32
  // CHECK: llvm.load %[[A]]
  // Two checks in one function share the panic block.
  // CHECK: llvm.cond_br %{{.*}}, ^{{.*}}, ^[[PANIC]](%arg3, %arg2 : i64, i64)
  // CHECK: ^[[PANIC]](%[[I:.*]]: i64, %[[N:.*]]: i64):
  // CHECK: llvm.call @runtime.panicindex(%[[I]], %[[N]]) : (i64, i64) -> ()
  // CHECK: llvm.unreachable
  llvm.func @idx(%p: !llvm.ptr, %i: i64, %n: i64, %j: i64) -> i32 {
    %a = go.index %p[%i] len %n, i32 : i64
    %v = llvm.load %a : !llvm.ptr -> i32
    go.bounds_check %j, %n : i64
    llvm.return %v : i32
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

  // ROUND-LABEL: llvm.func @slice3
  // ROUND: go.slice %arg0[%arg1 : %arg2 : %arg3] cap %arg4, i64 : i64 -> !llvm.struct<(ptr, i64, i64)>
  // CHECK-LABEL: llvm.func @slice3
  // CHECK: llvm.icmp "ule" %arg1, %arg2
  // CHECK: llvm.icmp "ule" %arg2, %arg3
  // CHECK: llvm.icmp "ule" %arg3, %arg4
  // CHECK: llvm.cond_br {{.*}} ^[[PANIC:.*]](%arg1, %arg2, %arg4 : i64, i64, i64)
  // CHECK: %[[LEN:.*]] = llvm.sub %arg2, %arg1
  // CHECK: %[[CAP:.*]] = llvm.sub %arg3, %arg1
  // CHECK: %[[MOVED:.*]] = llvm.getelementptr %arg0[%arg1] : (!llvm.ptr, i64) -> !llvm.ptr, i64
  // CHECK: %[[EMPTY:.*]] = llvm.icmp "eq" %[[CAP]]
  // CHECK: llvm.select %[[EMPTY]], %arg0, %[[MOVED]]
  // CHECK: llvm.call @runtime.panicslice
  llvm.func @slice3(%p: !llvm.ptr, %lo: i64, %hi: i64, %max: i64, %cap: i64) -> !llvm.struct<(ptr, i64, i64)> {
    %s = go.slice %p[%lo : %hi : %max] cap %cap, i64 : i64 -> !llvm.struct<(ptr, i64, i64)>
    llvm.return %s : !llvm.struct<(ptr, i64, i64)>
  }

  // ROUND-LABEL: llvm.func @substr
  // ROUND: go.slice %arg0[%arg1 : %arg2] cap %arg3, i8 : i64 -> !llvm.struct<(ptr, i64)>
  // CHECK-LABEL: llvm.func @substr
  // CHECK-NOT: llvm.sub %arg3
  // CHECK: llvm.insertvalue
  // CHECK-NOT: [2]
  // CHECK: llvm.return
  llvm.func @substr(%p: !llvm.ptr, %lo: i64, %hi: i64, %n: i64) -> !llvm.struct<(ptr, i64)> {
    %s = go.slice %p[%lo : %hi] cap %n, i8 : i64 -> !llvm.struct<(ptr, i64)>
    llvm.return %s : !llvm.struct<(ptr, i64)>
  }
  // CHECK: llvm.func @runtime.panicindex(i64, i64) attributes {passthrough = ["noreturn", "cold", "nounwind"]}
}

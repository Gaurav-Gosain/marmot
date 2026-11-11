// RUN: %marmot-opt --go-to-llvm %s | %FileCheck %s
// RUN: %marmot-opt %s | %marmot-opt | %FileCheck %s --check-prefix=ROUND
!str = !llvm.struct<(ptr, i64)>
module {
  llvm.mlir.global external constant @"type:map[string]bool"() : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:int"() : !llvm.array<56 x i8>

  // ROUND-LABEL: llvm.func @maps
  // ROUND: %[[M:.*]] = go.make_map @"type:map[string]bool", %arg1
  // ROUND: go.map_update %[[M]][%arg0] = %arg2, @"type:map[string]bool" : !llvm.struct<(ptr, i64)>, i1
  // ROUND: go.map_lookup %[[M]][%arg0], @"type:map[string]bool" : !llvm.struct<(ptr, i64)> -> i1
  // ROUND: go.map_lookup %[[M]][%arg0], @"type:map[string]bool" commaok : !llvm.struct<(ptr, i64)> -> i1
  // ROUND: go.map_delete %[[M]][%arg0], @"type:map[string]bool" : !llvm.struct<(ptr, i64)>
  // CHECK-LABEL: llvm.func @maps
  // CHECK: %[[T:.*]] = llvm.mlir.addressof @"type:map[string]bool"
  // CHECK: %[[M:.*]] = llvm.call @runtime.makemap(%[[T]], %arg1) : (!llvm.ptr, i64) -> !llvm.ptr
  // CHECK: llvm.intr.lifetime.start %[[K:.*]] :
  // CHECK: llvm.store %arg0, %[[K]] : !llvm.struct<(ptr, i64)>, !llvm.ptr
  // CHECK: %[[S:.*]] = llvm.call @runtime.mapassign(%{{.*}}, %[[M]], %[[K]])
  // CHECK: llvm.intr.lifetime.end %[[K]]
  // CHECK: %[[B:.*]] = llvm.zext %arg2 : i1 to i8
  // CHECK: llvm.store %[[B]], %[[S]] : i8, !llvm.ptr
  // CHECK: %[[P:.*]] = llvm.call @runtime.mapaccess1(
  // CHECK: %[[V8:.*]] = llvm.load %[[P]] : !llvm.ptr -> i8
  // CHECK: llvm.trunc %[[V8]] : i8 to i1
  // CHECK: %[[R2:.*]] = llvm.call @runtime.mapaccess2({{.*}}) : (!llvm.ptr, !llvm.ptr, !llvm.ptr) -> !llvm.struct<(ptr, i1)>
  // CHECK: llvm.extractvalue %[[R2]][1]
  // CHECK: llvm.call @runtime.mapdelete(
  llvm.func @maps(%k: !str, %hint: i64, %v: i1) -> i1 {
    %m = go.make_map @"type:map[string]bool", %hint
    go.map_update %m[%k] = %v, @"type:map[string]bool" : !str, i1
    %x = go.map_lookup %m[%k], @"type:map[string]bool" : !str -> i1
    %y, %ok = go.map_lookup %m[%k], @"type:map[string]bool" commaok : !str -> i1
    go.map_delete %m[%k], @"type:map[string]bool" : !str
    %r = llvm.and %x, %ok : i1
    llvm.return %r : i1
  }

  // ROUND-LABEL: llvm.func @chans
  // ROUND: %[[C:.*]] = go.make_chan @"type:int", %arg0
  // ROUND: go.chan_send %[[C]], %arg0 : i64
  // ROUND: go.chan_recv %[[C]] : i64
  // ROUND: go.chan_recv %[[C]] commaok : i64
  // CHECK-LABEL: llvm.func @chans
  // CHECK: %[[C:.*]] = llvm.call @runtime.makechan(%{{.*}}, %arg0)
  // CHECK: llvm.call @runtime.chansend1(%[[C]], %{{.*}}) : (!llvm.ptr, !llvm.ptr) -> ()
  // CHECK: llvm.call @runtime.chanrecv1(%[[C]], %[[S:.*]]) : (!llvm.ptr, !llvm.ptr) -> ()
  // CHECK: llvm.load %[[S]] : !llvm.ptr -> i64
  // CHECK: llvm.call @runtime.chanrecv2(%[[C]], %{{.*}}) : (!llvm.ptr, !llvm.ptr) -> i1
  llvm.func @chans(%n: i64) -> i64 {
    %c = go.make_chan @"type:int", %n
    go.chan_send %c, %n : i64
    %v = go.chan_recv %c : i64
    %w, %ok = go.chan_recv %c commaok : i64
    llvm.return %v : i64
  }
}

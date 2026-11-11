// RUN: %marmot-opt --go-devirt %s | %FileCheck %s
// RUN: %marmot-opt --go-devirt --go-escape %s | %FileCheck %s --check-prefix=ESC
!itab = !llvm.struct<(ptr, ptr, i32, i32, array<2 x ptr>)>
module {
  llvm.mlir.global external constant @"type:I"() : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:*T"() : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:int"() : !llvm.array<56 x i8>
  llvm.mlir.global linkonce_odr constant @"itab:*T,I"() : !itab {
    %u = llvm.mlir.undef : !llvm.array<2 x ptr>
    %f0 = llvm.mlir.addressof @"T.A" : !llvm.ptr
    %f1 = llvm.mlir.addressof @"T.B" : !llvm.ptr
    %a1 = llvm.insertvalue %f0, %u[0] : !llvm.array<2 x ptr>
    %a2 = llvm.insertvalue %f1, %a1[1] : !llvm.array<2 x ptr>
    %s0 = llvm.mlir.undef : !itab
    %i = llvm.mlir.addressof @"type:I" : !llvm.ptr
    %t = llvm.mlir.addressof @"type:*T" : !llvm.ptr
    %s1 = llvm.insertvalue %i, %s0[0] : !itab
    %s2 = llvm.insertvalue %t, %s1[1] : !itab
    %s3 = llvm.insertvalue %a2, %s2[4] : !itab
    llvm.return %s3 : !itab
  }
  // A weak (replaceable) itab must not be trusted.
  llvm.mlir.global weak constant @"itab:weak"() : !itab {
    %s0 = llvm.mlir.zero : !itab
    %f1 = llvm.mlir.addressof @"T.B" : !llvm.ptr
    %s1 = llvm.insertvalue %f1, %s0[4, 1] : !itab
    llvm.return %s1 : !itab
  }
  llvm.func @"T.A"(%t: !llvm.ptr) -> i64 {
    %v = llvm.load %t : !llvm.ptr -> i64
    llvm.return %v : i64
  }
  llvm.func @"T.B"(%t: !llvm.ptr, %x: i64) {
    llvm.store %x, %t : i64, !llvm.ptr
    llvm.return
  }

  // CHECK-LABEL: llvm.func @calls
  // CHECK: %[[P:.*]] = go.new
  // CHECK: llvm.call @T.A(%[[P]]) : (!llvm.ptr) -> i64
  // CHECK: llvm.call @T.B(%[[P]], %arg0) : (!llvm.ptr, i64) -> ()
  // CHECK: go.iface_call %arg1[0]()
  // The receiver no longer escapes through the interface call.
  // ESC-LABEL: llvm.func @calls
  // ESC: go.new @"type:*T", i64 stack
  llvm.func @calls(%x: i64, %other: !llvm.struct<(ptr, ptr)>) -> i64 {
    %p = go.new @"type:*T", i64
    %i = go.make_iface @"itab:*T,I", %p
    %a = go.iface_call %i[0]() : () -> i64
    go.iface_call %i[1](%x) : (i64) -> ()
    %o = go.iface_call %other[0]() : () -> i64
    %r = llvm.add %a, %o : i64
    llvm.return %r : i64
  }

  // CHECK-LABEL: llvm.func @weak
  // CHECK: go.iface_call %{{.*}}[1](%arg1)
  llvm.func @weak(%p: !llvm.ptr, %x: i64) {
    %w = go.make_iface @"itab:weak", %p
    go.iface_call %w[1](%x) : (i64) -> ()
    llvm.return
  }

  // CHECK-LABEL: llvm.func @asserts
  // CHECK-NOT: go.type_assert %{{.*}} -> @"type:*T" from_iface
  // CHECK: %[[T:.*]] = llvm.mlir.constant(true) : i1
  // CHECK: go.type_assert %{{.*}}, @"type:I" -> @"type:int" from_iface commaok
  // CHECK: llvm.return %arg0
  llvm.func @asserts(%p: !llvm.ptr) -> !llvm.ptr {
    %i = go.make_iface @"itab:*T,I", %p
    %d = go.type_assert %i, @"type:I" -> @"type:*T" from_iface
    %e = go.make_iface @"type:*T", %p
    %d2, %ok = go.type_assert %e, @"type:any" -> @"type:*T" commaok
    %d3, %ok3 = go.type_assert %i, @"type:I" -> @"type:int" from_iface commaok
    llvm.return %d : !llvm.ptr
  }
}

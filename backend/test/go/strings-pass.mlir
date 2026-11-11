// RUN: %marmot-opt --go-strings %s | %FileCheck %s
!str = !llvm.struct<(ptr, i64)>
!slice = !llvm.struct<(ptr, i64, i64)>
module {
  llvm.mlir.global private unnamed_addr constant @".str.0"("hello") {addr_space = 0 : i32}
  llvm.mlir.global private unnamed_addr constant @".str.1"(", ") {addr_space = 0 : i32}
  llvm.mlir.global external constant @"type:map"() : !llvm.array<56 x i8>
  llvm.func @runtime.strequal(!str, !str) -> i1
  llvm.func @sink(!str)

  // New constants go to the start of the module.
  // CHECK: llvm.mlir.global private unnamed_addr constant @".str.marmot.{{[0-9a-f]+}}"("hello, ")

  // ("hello" + ", ") + s + "" -> ".str.marmot" + s, flattened and folded.
  // CHECK-LABEL: llvm.func @fold
  // CHECK: %[[A:.*]] = llvm.mlir.addressof @".str.marmot.{{[0-9a-f]+}}" : !llvm.ptr
  // CHECK: %[[N:.*]] = llvm.mlir.constant(7 : i64) : i64
  // CHECK: %[[K:.*]] = llvm.insertvalue %[[N]]
  // CHECK: %[[R:.*]] = go.str_concat %[[K]], %arg0
  // CHECK-NOT: go.str_concat
  // CHECK: llvm.return %[[R]]
  llvm.func @fold(%s: !str) -> !str {
    %p0 = llvm.mlir.addressof @".str.0" : !llvm.ptr
    %n0 = llvm.mlir.constant(5 : i64) : i64
    %u0 = llvm.mlir.undef : !str
    %a0 = llvm.insertvalue %p0, %u0[0] : !str
    %h = llvm.insertvalue %n0, %a0[1] : !str
    %p1 = llvm.mlir.addressof @".str.1" : !llvm.ptr
    %n1 = llvm.mlir.constant(2 : i64) : i64
    %u1 = llvm.mlir.undef : !str
    %a1 = llvm.insertvalue %p1, %u1[0] : !str
    %c = llvm.insertvalue %n1, %a1[1] : !str
    %e = llvm.mlir.zero : !str
    %hc = go.str_concat %h, %c
    %all = go.str_concat %hc, %s, %e
    llvm.return %all : !str
  }

  // An inner concat with another use is kept.
  // CHECK-LABEL: llvm.func @shared
  // CHECK: %[[I:.*]] = go.str_concat %arg0, %arg1
  // CHECK: go.str_concat %[[I]], %arg0
  llvm.func @shared(%a: !str, %b: !str) -> !str {
    %i = go.str_concat %a, %b
    llvm.call @sink(%i) : (!str) -> ()
    %o = go.str_concat %i, %a
    llvm.return %o : !str
  }

  // m[string(b)] and string(b) == s: no copy. Stored or passed on: copy.
  // CHECK-LABEL: llvm.func @nocopy
  // CHECK: go.bytes_to_str %arg1 nocopy
  // CHECK: go.bytes_to_str %arg1 nocopy
  // CHECK: go.bytes_to_str %arg1{{$}}
  llvm.func @nocopy(%m: !llvm.ptr, %b: !slice, %s: !str) -> i64 {
    %k = go.bytes_to_str %b
    %v = go.map_lookup %m[%k], @"type:map" : !str -> i64
    %t = go.bytes_to_str %b
    %eq = llvm.call @runtime.strequal(%t, %s) : (!str, !str) -> i1
    %u = go.bytes_to_str %b
    llvm.call @sink(%u) : (!str) -> ()
    llvm.return %v : i64
  }

  // A write to memory between the conversion and its use: copy.
  // CHECK-LABEL: llvm.func @clobber
  // CHECK: go.bytes_to_str %arg1{{$}}
  llvm.func @clobber(%m: !llvm.ptr, %b: !slice, %p: !llvm.ptr) -> i64 {
    %k = go.bytes_to_str %b
    %c = llvm.mlir.constant(0 : i8) : i8
    llvm.store %c, %p : i8, !llvm.ptr
    %v = go.map_lookup %m[%k], @"type:map" : !str -> i64
    llvm.return %v : i64
  }
}

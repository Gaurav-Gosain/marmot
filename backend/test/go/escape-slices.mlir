// Slice parameters get summaries too: a local array sliced and passed to a
// function that only reads through the slice stays on the stack.
// RUN: %marmot-opt --go-escape %s | %FileCheck %s --check-prefix=SLICE
// SLICE-LABEL: llvm.func @sum(%arg0: !llvm.struct<(ptr, i64, i64)> {go.noescape})
// SLICE-LABEL: llvm.func @keepSlice(%arg0: !llvm.struct<(ptr, i64, i64)>)
// SLICE-LABEL: llvm.func @callsSum
// SLICE: go.new @"type:int", !llvm.array<24 x i8> stack
// SLICE: go.new @"type:int", !llvm.array<24 x i8>{{$}}
module {
  llvm.mlir.global external constant @"type:int"() : !llvm.array<56 x i8>
  llvm.mlir.global internal @sink2() : !llvm.ptr {
    %z = llvm.mlir.zero : !llvm.ptr
    llvm.return %z : !llvm.ptr
  }
  llvm.func @sum(%s: !llvm.struct<(ptr, i64, i64)>) -> i8 {
    %p = llvm.extractvalue %s[0] : !llvm.struct<(ptr, i64, i64)>
    %v = llvm.load %p : !llvm.ptr -> i8
    llvm.return %v : i8
  }
  llvm.func @keepSlice(%s: !llvm.struct<(ptr, i64, i64)>) {
    %p = llvm.extractvalue %s[0] : !llvm.struct<(ptr, i64, i64)>
    %g = llvm.mlir.addressof @sink2 : !llvm.ptr
    llvm.store %p, %g : !llvm.ptr, !llvm.ptr
    llvm.return
  }
  llvm.func @callsSum(%n: i64) -> i8 {
    %a = go.new @"type:int", !llvm.array<24 x i8>
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c24 = llvm.mlir.constant(24 : i64) : i64
    %s = go.slice %a[%c0 : %n] cap %c24, i8 len_bound : i64 -> !llvm.struct<(ptr, i64, i64)>
    %v = llvm.call @sum(%s) : (!llvm.struct<(ptr, i64, i64)>) -> i8
    %b = go.new @"type:int", !llvm.array<24 x i8>
    %t = go.slice %b[%c0 : %n] cap %c24, i8 len_bound : i64 -> !llvm.struct<(ptr, i64, i64)>
    llvm.call @keepSlice(%t) : (!llvm.struct<(ptr, i64, i64)>) -> ()
    llvm.return %v : i8
  }
}

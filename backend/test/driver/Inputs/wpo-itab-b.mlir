module {
  llvm.mlir.global linkonce_odr constant @"itab:b.T,b.I"() : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)> {
    %z = llvm.mlir.zero : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
    %m = llvm.mlir.addressof @"b.T.M" : !llvm.ptr
    %r = llvm.insertvalue %m, %z[4, 0] : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
    llvm.return %r : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
  }
  // Only the itab references this method.
  llvm.func @"b.T.M"(%x: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(5 : i64) : i64
    %r = llvm.add %x, %c : i64
    llvm.return %r : i64
  }
  // Small: imported into the caller with a copy of the itab; LLVM then folds
  // the slot load into a direct call to b.T.M.
  llvm.func @"b.Call"(%x: i64) -> i64 {
    %t = llvm.mlir.addressof @"itab:b.T,b.I" : !llvm.ptr
    %s = llvm.getelementptr %t[0, 4, 0] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
    %f = llvm.load %s : !llvm.ptr -> !llvm.ptr
    %r = llvm.call %f(%x) : !llvm.ptr, (i64) -> i64
    llvm.return %r : i64
  }
}

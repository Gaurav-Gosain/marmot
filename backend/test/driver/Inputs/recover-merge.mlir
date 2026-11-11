module {
  llvm.mlir.global external @sink() : !llvm.struct<(ptr, ptr)> {
    %z = llvm.mlir.zero : !llvm.struct<(ptr, ptr)>
    llvm.return %z : !llvm.struct<(ptr, ptr)>
  }
  llvm.func @"a$1"(%env: !llvm.ptr) {
    %r = go.recover
    %s = llvm.mlir.addressof @sink : !llvm.ptr
    llvm.store %r, %s : !llvm.struct<(ptr, ptr)>, !llvm.ptr
    llvm.return
  }
  llvm.func @"b$1"(%env: !llvm.ptr) {
    %r = go.recover
    %s = llvm.mlir.addressof @sink : !llvm.ptr
    llvm.store %r, %s : !llvm.struct<(ptr, ptr)>, !llvm.ptr
    llvm.return
  }
}

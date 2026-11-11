module {
  llvm.mlir.global external @"c.sink"() : !llvm.ptr {
    %z = llvm.mlir.zero : !llvm.ptr
    llvm.return %z : !llvm.ptr
  }
  llvm.func @"c.Keep"(%p: !llvm.ptr) attributes {passthrough = ["noinline"]} {
    %s = llvm.mlir.addressof @"c.sink" : !llvm.ptr
    llvm.store %p, %s : !llvm.ptr, !llvm.ptr
    llvm.return
  }
}

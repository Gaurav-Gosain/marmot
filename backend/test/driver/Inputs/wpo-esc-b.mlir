module {
  // Reads through its parameter only: no escape.
  llvm.func @"b.Read"(%p: !llvm.ptr) -> i64 attributes {passthrough = ["noinline"]} {
    %v = llvm.load %p : !llvm.ptr -> i64
    llvm.return %v : i64
  }
  // Calls c.Keep, which stores its parameter: escapes (learned in a later round).
  llvm.func @"c.Keep"(!llvm.ptr)
  llvm.func @"b.Pass"(%p: !llvm.ptr) attributes {passthrough = ["noinline"]} {
    llvm.call @"c.Keep"(%p) : (!llvm.ptr) -> ()
    llvm.return
  }
}

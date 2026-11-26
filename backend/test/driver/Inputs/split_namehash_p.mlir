// split-namehash.mlir: package p, the same names and code as package q.
module attributes {marmot.package = "p"} {
  llvm.mlir.global internal @counter(1 : i64) : i64
  llvm.mlir.global_ctors ctors = [@init], priorities = [65535 : i32], data = [#llvm.zero]
  llvm.func internal @bump() -> i64 attributes {passthrough = ["noinline"]} {
    %p = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %p : !llvm.ptr -> i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %w = llvm.add %v, %one : i64
    llvm.store %w, %p : i64, !llvm.ptr
    llvm.return %w : i64
  }
  llvm.func internal @twice() -> i64 attributes {passthrough = ["noinline"]} {
    %a = llvm.call @bump() : () -> i64
    %b = llvm.call @bump() : () -> i64
    %c = llvm.mlir.constant(7 : i64) : i64
    %s = llvm.add %a, %b : i64
    %r = llvm.mul %s, %c : i64
    llvm.return %r : i64
  }
  llvm.func internal @init() attributes {passthrough = ["noinline"]} {
    %r = llvm.call @twice() : () -> i64
    %p = llvm.mlir.addressof @counter : !llvm.ptr
    llvm.store %r, %p : i64, !llvm.ptr
    llvm.return
  }
}

module {
  llvm.mlir.global internal @counter(0 : i64) {addr_space = 0 : i32} : i64
  llvm.func @helper(%x: i64) -> i64 {
    %one = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.add %x, %one : i64
    llvm.return %r : i64
  }
  // An init function between two ordinary ones: moved to the init section,
  // calling back into text and touching data.
  llvm.func @pkg.init() attributes {section = "INITSECT"} {
    %c = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %c : !llvm.ptr -> i64
    %w = llvm.call @helper(%v) : (i64) -> i64
    %t = llvm.call @twice(%w) : (i64) -> i64
    llvm.store %t, %c : i64, !llvm.ptr
    llvm.return
  }
  llvm.func @twice(%x: i64) -> i64 {
    %r = llvm.add %x, %x : i64
    llvm.return %r : i64
  }
  llvm.func @get() -> i64 {
    %c = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %c : !llvm.ptr -> i64
    %h = llvm.call @helper(%v) : (i64) -> i64
    llvm.return %h : i64
  }
}

module {
  llvm.mlir.global internal @counter(0 : i64) : i64
  // Small, called from the other module: imported and inlined there.
  llvm.func @"b.Small"(%x: i64) -> i64 {
    %c = llvm.mlir.constant(3 : i64) : i64
    %r = llvm.mul %x, %c : i64
    llvm.return %r : i64
  }
  // Touches an internal global: not imported, stays a call.
  llvm.func @"b.Count"() -> i64 {
    %p = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %p : !llvm.ptr -> i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %w = llvm.add %v, %one : i64
    llvm.store %w, %p : i64, !llvm.ptr
    llvm.return %w : i64
  }
  // Used only inside this module: internalized (no global symbol).
  llvm.func @"b.helper"(%x: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %r = llvm.add %x, %x : i64
    llvm.return %r : i64
  }
  llvm.func @"b.UsesHelper"(%x: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %r = llvm.call @"b.helper"(%x) : (i64) -> i64
    llvm.return %r : i64
  }
  // Reached by nobody: dropped.
  llvm.func @"b.Unused"() -> i64 {
    %r = llvm.call @"b.Count"() : () -> i64
    llvm.return %r : i64
  }
}

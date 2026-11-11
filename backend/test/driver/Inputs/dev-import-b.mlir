// Another chunk (source file) of package p, for dev-import.mlir.
module attributes {marmot.package = "p"} {
  // Small: imported into the other chunk and inlined there.
  llvm.func @"p.Small"(%x: i64) -> i64 {
    %c = llvm.mlir.constant(3 : i64) : i64
    %r = llvm.mul %x, %c : i64
    llvm.return %r : i64
  }
  llvm.func @"p.Big"(%x: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c = llvm.mlir.constant(7 : i64) : i64
    %r = llvm.add %x, %c : i64
    llvm.return %r : i64
  }
}

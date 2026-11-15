module {
  // Pointer-free globals in the sections the garbage collector skips.
  llvm.mlir.global external @np_data(5 : i64) {addr_space = 0 : i32, alignment = 8 : i64, section = "DATASECT"} : i64
  llvm.mlir.global external @np_bss() {addr_space = 0 : i32, alignment = 8 : i64, section = "BSSSECT"} : !llvm.array<4 x i64> {
    %z = llvm.mlir.zero : !llvm.array<4 x i64>
    llvm.return %z : !llvm.array<4 x i64>
  }
  // np_data += 1; np_bss[3] += 2; returns np_data + np_bss[3].
  llvm.func @np_bump() -> i64 {
    %d = llvm.mlir.addressof @np_data : !llvm.ptr
    %b = llvm.mlir.addressof @np_bss : !llvm.ptr
    %one = llvm.mlir.constant(1 : i64) : i64
    %two = llvm.mlir.constant(2 : i64) : i64
    %x = llvm.load %d : !llvm.ptr -> i64
    %x1 = llvm.add %x, %one : i64
    llvm.store %x1, %d : i64, !llvm.ptr
    %p = llvm.getelementptr %b[0, 3] : (!llvm.ptr) -> !llvm.ptr, !llvm.array<4 x i64>
    %y = llvm.load %p : !llvm.ptr -> i64
    %y1 = llvm.add %y, %two : i64
    llvm.store %y1, %p : i64, !llvm.ptr
    %r = llvm.add %x1, %y1 : i64
    llvm.return %r : i64
  }
}

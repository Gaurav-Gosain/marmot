// Type link entries: each is kept only while its descriptor is live
// (tlinks_main.c). The section name is filled in per object format.
module {
  llvm.mlir.global internal constant @desc_live(7 : i64) {addr_space = 0 : i32, alignment = 8 : i64} : i64
  llvm.mlir.global internal constant @desc_dead(9 : i64) {addr_space = 0 : i32, alignment = 8 : i64} : i64
  llvm.mlir.global internal constant @tl_live() {addr_space = 0 : i32, alignment = 8 : i64, section = "SECTION"} : !llvm.ptr {
    %0 = llvm.mlir.addressof @desc_live : !llvm.ptr
    llvm.return %0 : !llvm.ptr
  }
  llvm.mlir.global internal constant @tl_dead() {addr_space = 0 : i32, alignment = 8 : i64, section = "SECTION"} : !llvm.ptr {
    %0 = llvm.mlir.addressof @desc_dead : !llvm.ptr
    llvm.return %0 : !llvm.ptr
  }
  llvm.mlir.global appending @llvm.compiler.used() {addr_space = 0 : i32, section = "llvm.metadata"} : !llvm.array<2 x ptr> {
    %0 = llvm.mlir.undef : !llvm.array<2 x ptr>
    %1 = llvm.mlir.addressof @tl_live : !llvm.ptr
    %2 = llvm.insertvalue %1, %0[0] : !llvm.array<2 x ptr>
    %3 = llvm.mlir.addressof @tl_dead : !llvm.ptr
    %4 = llvm.insertvalue %3, %2[1] : !llvm.array<2 x ptr>
    llvm.return %4 : !llvm.array<2 x ptr>
  }
  llvm.func @get_live() -> !llvm.ptr {
    %0 = llvm.mlir.addressof @desc_live : !llvm.ptr
    llvm.return %0 : !llvm.ptr
  }
}

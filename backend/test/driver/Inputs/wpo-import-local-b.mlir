// wpo-import-local.mlir: b.Small (importable) calls b.first, a linkonce_odr
// function that reads a private constant of this module.
module {
  llvm.mlir.global private constant @".str.1"("hi\00") {addr_space = 0 : i32}
  llvm.func linkonce_odr @"b.first"() -> i64 {
    %p = llvm.mlir.addressof @".str.1" : !llvm.ptr
    %c = llvm.load %p : !llvm.ptr -> i8
    %r = llvm.zext %c : i8 to i64
    llvm.return %r : i64
  }
  llvm.func @"b.Small"(%x: i64) -> i64 {
    %f = llvm.call @"b.first"() : () -> i64
    %r = llvm.add %f, %x : i64
    llvm.return %r : i64
  }
}

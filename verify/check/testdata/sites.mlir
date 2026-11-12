module {
  llvm.mlir.global linkonce_odr constant @"type:p.Ptr.gcdata"() {addr_space = 0 : i32} : !llvm.array<1 x i8> {
    %c1 = llvm.mlir.constant(2 : i8) : i8
    %c2 = llvm.mlir.undef : !llvm.array<1 x i8>
    %c3 = llvm.insertvalue %c1, %c2[0] : !llvm.array<1 x i8>
    llvm.return %c3 : !llvm.array<1 x i8>
  }
  llvm.mlir.global linkonce_odr constant @"type:p.Ptr"() {addr_space = 0 : i32} : !llvm.struct<(i64, i64, ptr)> {
    %c1 = llvm.mlir.constant(16 : i64) : i64
    %c2 = llvm.mlir.constant(16 : i64) : i64
    %c3 = llvm.mlir.addressof @"type:p.Ptr.gcdata" : !llvm.ptr
    llvm.return %c3 : !llvm.ptr
  }
  llvm.mlir.global linkonce_odr constant @"type:p.Num"() {addr_space = 0 : i32} : !llvm.struct<(i64, i64, ptr)> {
    %c1 = llvm.mlir.constant(16 : i64) : i64
    %c2 = llvm.mlir.constant(0 : i64) : i64
    %c3 = llvm.mlir.zero : !llvm.ptr
    llvm.return %c3 : !llvm.ptr
  }
  llvm.func @f() {
    %a = go.new @"type:p.Ptr", !llvm.struct<(i64, ptr)>
    %b = go.new @"type:p.Num", !llvm.struct<(i64, ptr)>
    %c = go.new @"type:p.Ptr", !llvm.struct<(ptr, i64)>
    llvm.return
  }
}

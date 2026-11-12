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
  llvm.func @runtime.newobject(!llvm.ptr) -> !llvm.ptr
  llvm.func @runtime.growslice(!llvm.ptr, !llvm.ptr, i64, i64, i64) -> !llvm.struct<(ptr, i64, i64)>
  llvm.func @rt_region_new_noscan() -> !llvm.ptr
  llvm.func @rt_region_alloc(!llvm.ptr, i64) -> !llvm.ptr
  llvm.func @good(%x: !llvm.ptr, %i: i64) {
    %d = llvm.mlir.addressof @"type:p.Ptr" : !llvm.ptr
    %o = llvm.call @runtime.newobject(%d) : (!llvm.ptr) -> !llvm.ptr
    %f = llvm.getelementptr inbounds %o[0, 1] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(i64, ptr)>
    llvm.store %x, %f {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr
    %s = llvm.call @runtime.growslice(%d, %x, %i, %i, %i) : (!llvm.ptr, !llvm.ptr, i64, i64, i64) -> !llvm.struct<(ptr, i64, i64)>
    %sp = llvm.extractvalue %s[0] : !llvm.struct<(ptr, i64, i64)>
    llvm.br ^b1(%sp : !llvm.ptr)
  ^b1(%q: !llvm.ptr):
    %e = llvm.getelementptr inbounds %q[%i, 1] : (!llvm.ptr, i64) -> !llvm.ptr, !llvm.struct<(i64, ptr)>
    llvm.store %x, %e {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr
    llvm.return
  }
  llvm.func @bad(%x: !llvm.ptr, %i: i64) {
    %d = llvm.mlir.addressof @"type:p.Num" : !llvm.ptr
    %o = llvm.call @runtime.newobject(%d) : (!llvm.ptr) -> !llvm.ptr
    %f = llvm.getelementptr inbounds %o[0, 1] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(i64, ptr)>
    llvm.store %x, %f {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr
    %p = llvm.mlir.addressof @"type:p.Ptr" : !llvm.ptr
    %s = llvm.call @runtime.growslice(%p, %x, %i, %i, %i) : (!llvm.ptr, !llvm.ptr, i64, i64, i64) -> !llvm.struct<(ptr, i64, i64)>
    %sp = llvm.extractvalue %s[0] : !llvm.struct<(ptr, i64, i64)>
    llvm.br ^b1(%sp : !llvm.ptr)
  ^b1(%q: !llvm.ptr):
    %e = llvm.getelementptr inbounds %q[%i] : (!llvm.ptr, i64) -> !llvm.ptr, !llvm.struct<(i64, ptr)>
    llvm.store %x, %e {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr
    %r = llvm.call @rt_region_new_noscan() : () -> !llvm.ptr
    %c16 = llvm.mlir.constant(16 : i64) : i64
    %ro = llvm.call @rt_region_alloc(%r, %c16) : (!llvm.ptr, i64) -> !llvm.ptr
    llvm.store %x, %ro {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr
    llvm.return
  }
}

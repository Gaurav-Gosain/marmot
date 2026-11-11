// -wpo drops the fields and methods tables of types whose descriptor is only
// used to allocate, when no reflect code is live.
// RUN: printf '%s %t.ll\n' > %t.jobs
// RUN: %backend -O2 -emit=llvm -batch %t.jobs -wpo -time 2> %t.time
// RUN: %FileCheck %s < %t.ll
// RUN: %FileCheck %s --check-prefix=STATS < %t.time
// STATS: 1 types without reflection tables
// CHECK-NOT: @"type:main.T.fields"
// CHECK: @"type:main.U.fields" =
// CHECK: @"type:main.T" = linkonce_odr constant {{.*}} zeroinitializer
!rt = !llvm.struct<(i64, i64, ptr, ptr)>
!st = !llvm.struct<(!rt, ptr, i64)>
module {
  llvm.mlir.global linkonce_odr constant @"type:main.T.fields"(dense<1> : tensor<16xi8>) : !llvm.array<16 x i8>
  llvm.mlir.global linkonce_odr constant @"type:main.U.fields"(dense<2> : tensor<16xi8>) : !llvm.array<16 x i8>
  llvm.mlir.global linkonce_odr constant @"type:main.T"() : !st {
    %z = llvm.mlir.zero : !st
    %f = llvm.mlir.addressof @"type:main.T.fields" : !llvm.ptr
    %n = llvm.mlir.constant(2 : i64) : i64
    %a = llvm.insertvalue %f, %z[1] : !st
    %b = llvm.insertvalue %n, %a[2] : !st
    llvm.return %b : !st
  }
  llvm.mlir.global linkonce_odr constant @"type:main.U"() : !st {
    %z = llvm.mlir.zero : !st
    %f = llvm.mlir.addressof @"type:main.U.fields" : !llvm.ptr
    %n = llvm.mlir.constant(2 : i64) : i64
    %a = llvm.insertvalue %f, %z[1] : !st
    %b = llvm.insertvalue %n, %a[2] : !st
    llvm.return %b : !st
  }
  llvm.mlir.global external @sink() : !llvm.struct<(ptr, ptr)> {
    %z = llvm.mlir.zero : !llvm.struct<(ptr, ptr)>
    llvm.return %z : !llvm.struct<(ptr, ptr)>
  }
  llvm.func @runtime.newobject(!llvm.ptr) -> !llvm.ptr
  llvm.func @main() -> i32 {
    // T: allocation only.
    %t = llvm.mlir.addressof @"type:main.T" : !llvm.ptr
    %p = llvm.call @runtime.newobject(%t) : (!llvm.ptr) -> !llvm.ptr
    // U: becomes an interface value, so typeequal or getitab may read it.
    %u = llvm.mlir.addressof @"type:main.U" : !llvm.ptr
    %e0 = llvm.mlir.poison : !llvm.struct<(ptr, ptr)>
    %e1 = llvm.insertvalue %u, %e0[0] : !llvm.struct<(ptr, ptr)>
    %e2 = llvm.insertvalue %p, %e1[1] : !llvm.struct<(ptr, ptr)>
    %s = llvm.mlir.addressof @sink : !llvm.ptr
    llvm.store %e2, %s : !llvm.struct<(ptr, ptr)>, !llvm.ptr
    %r = llvm.mlir.constant(0 : i32) : i32
    llvm.return %r : i32
  }
}

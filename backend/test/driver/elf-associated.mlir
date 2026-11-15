// ELF: a marmot_tlinks entry is associated with its descriptor and a
// marmot_gcvars record with its variable (operand 0), so --gc-sections with
// -z start-stop-gc keeps them with what they describe. Mach-O gets none.
// RUN: %backend -O0 -target linux/amd64 -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -O0 -target darwin/arm64 -emit=llvm -o - %s | %FileCheck %s --check-prefix=MACHO
// CHECK-DAG: @tl = constant ptr @desc, section "marmot_tlinks"{{.*}}!associated ![[D:[0-9]+]]
// CHECK-DAG: @gv = constant { ptr, i64, ptr } { ptr @main.v, i64 8, ptr @bits }, section "marmot_gcvars"{{.*}}!associated ![[V:[0-9]+]]
// CHECK-DAG: ![[D]] = !{ptr @desc}
// CHECK-DAG: ![[V]] = !{ptr @main.v}
// MACHO-NOT: !associated
module {
  llvm.mlir.global external constant @desc(0 : i64) : i64
  llvm.mlir.global external constant @tl() {section = "marmot_tlinks"} : !llvm.ptr {
    %0 = llvm.mlir.addressof @desc : !llvm.ptr
    llvm.return %0 : !llvm.ptr
  }
  llvm.mlir.global external @main.v(0 : i64) : i64
  llvm.mlir.global external constant @bits(1 : i8) : i8
  llvm.mlir.global external constant @gv() {section = "marmot_gcvars"} : !llvm.struct<(ptr, i64, ptr)> {
    %0 = llvm.mlir.addressof @main.v : !llvm.ptr
    %1 = llvm.mlir.constant(8 : i64) : i64
    %2 = llvm.mlir.addressof @bits : !llvm.ptr
    %3 = llvm.mlir.undef : !llvm.struct<(ptr, i64, ptr)>
    %4 = llvm.insertvalue %0, %3[0] : !llvm.struct<(ptr, i64, ptr)>
    %5 = llvm.insertvalue %1, %4[1] : !llvm.struct<(ptr, i64, ptr)>
    %6 = llvm.insertvalue %2, %5[2] : !llvm.struct<(ptr, i64, ptr)>
    llvm.return %6 : !llvm.struct<(ptr, i64, ptr)>
  }
}

// Splitting keeps each marmot_gcvars record in the group of its variable,
// so every record's !associated names a definition of its own group (the
// verifier rejected a record whose variable was in another group).
// RUN: rm -rf %t.cache %t.d && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: %backend -O2 -target linux/amd64 -batch %t.jobs -split-large=1 -cache-dir %t.cache
// RUN: test -f %t.a.o.parts
// RUN: for o in $(cat %t.a.o.parts); do %llvm-readobj --sections $o; done > %t.secs
// RUN: python3 -c "import re,sys; s=open(sys.argv[1]).read().split('Section {'); g=[x for x in s if 'Name: marmot_gcvars' in x]; bad=[x for x in g if 'SHF_LINK_ORDER' in x and re.search(r'Link: 0\n', x)]; print('records', len(g), 'unlinked', len(bad))" %t.secs | %FileCheck %s
// CHECK: records 4 unlinked 0
module {
  llvm.mlir.global external @main.v0(0 : i64) : i64
  llvm.mlir.global external @main.v1(0 : i64) : i64
  llvm.mlir.global external @main.v2(0 : i64) : i64
  llvm.mlir.global external @main.v3(0 : i64) : i64
  llvm.mlir.global external constant @bits(1 : i8) : i8
  llvm.mlir.global external constant @main.r0() {section = "marmot_gcvars"} : !llvm.struct<(ptr, i64, ptr)> {
    %0 = llvm.mlir.addressof @main.v0 : !llvm.ptr
    %1 = llvm.mlir.constant(8 : i64) : i64
    %2 = llvm.mlir.addressof @bits : !llvm.ptr
    %3 = llvm.mlir.undef : !llvm.struct<(ptr, i64, ptr)>
    %4 = llvm.insertvalue %0, %3[0] : !llvm.struct<(ptr, i64, ptr)>
    %5 = llvm.insertvalue %1, %4[1] : !llvm.struct<(ptr, i64, ptr)>
    %6 = llvm.insertvalue %2, %5[2] : !llvm.struct<(ptr, i64, ptr)>
    llvm.return %6 : !llvm.struct<(ptr, i64, ptr)>
  }
  llvm.mlir.global external constant @main.r1() {section = "marmot_gcvars"} : !llvm.struct<(ptr, i64, ptr)> {
    %0 = llvm.mlir.addressof @main.v1 : !llvm.ptr
    %1 = llvm.mlir.constant(8 : i64) : i64
    %2 = llvm.mlir.addressof @bits : !llvm.ptr
    %3 = llvm.mlir.undef : !llvm.struct<(ptr, i64, ptr)>
    %4 = llvm.insertvalue %0, %3[0] : !llvm.struct<(ptr, i64, ptr)>
    %5 = llvm.insertvalue %1, %4[1] : !llvm.struct<(ptr, i64, ptr)>
    %6 = llvm.insertvalue %2, %5[2] : !llvm.struct<(ptr, i64, ptr)>
    llvm.return %6 : !llvm.struct<(ptr, i64, ptr)>
  }
  llvm.mlir.global external constant @main.r2() {section = "marmot_gcvars"} : !llvm.struct<(ptr, i64, ptr)> {
    %0 = llvm.mlir.addressof @main.v2 : !llvm.ptr
    %1 = llvm.mlir.constant(8 : i64) : i64
    %2 = llvm.mlir.addressof @bits : !llvm.ptr
    %3 = llvm.mlir.undef : !llvm.struct<(ptr, i64, ptr)>
    %4 = llvm.insertvalue %0, %3[0] : !llvm.struct<(ptr, i64, ptr)>
    %5 = llvm.insertvalue %1, %4[1] : !llvm.struct<(ptr, i64, ptr)>
    %6 = llvm.insertvalue %2, %5[2] : !llvm.struct<(ptr, i64, ptr)>
    llvm.return %6 : !llvm.struct<(ptr, i64, ptr)>
  }
  llvm.mlir.global external constant @main.r3() {section = "marmot_gcvars"} : !llvm.struct<(ptr, i64, ptr)> {
    %0 = llvm.mlir.addressof @main.v3 : !llvm.ptr
    %1 = llvm.mlir.constant(8 : i64) : i64
    %2 = llvm.mlir.addressof @bits : !llvm.ptr
    %3 = llvm.mlir.undef : !llvm.struct<(ptr, i64, ptr)>
    %4 = llvm.insertvalue %0, %3[0] : !llvm.struct<(ptr, i64, ptr)>
    %5 = llvm.insertvalue %1, %4[1] : !llvm.struct<(ptr, i64, ptr)>
    %6 = llvm.insertvalue %2, %5[2] : !llvm.struct<(ptr, i64, ptr)>
    llvm.return %6 : !llvm.struct<(ptr, i64, ptr)>
  }
  llvm.func @main.f0() -> i64 {
    %0 = llvm.mlir.addressof @main.v0 : !llvm.ptr
    %1 = llvm.load %0 : !llvm.ptr -> i64
    llvm.return %1 : i64
  }
  llvm.func @main.f1() -> i64 {
    %0 = llvm.mlir.addressof @main.v1 : !llvm.ptr
    %1 = llvm.load %0 : !llvm.ptr -> i64
    llvm.return %1 : i64
  }
  llvm.func @main.f2() -> i64 {
    %0 = llvm.mlir.addressof @main.v2 : !llvm.ptr
    %1 = llvm.load %0 : !llvm.ptr -> i64
    llvm.return %1 : i64
  }
  llvm.func @main.f3() -> i64 {
    %0 = llvm.mlir.addressof @main.v3 : !llvm.ptr
    %1 = llvm.load %0 : !llvm.ptr -> i64
    llvm.return %1 : i64
  }
}

// -Os and -Oz mark functions like clang does; package init stays optnone.
// RUN: %backend -Oz -emit=llvm -o - %s | %FileCheck %s --check-prefix=OZ
// RUN: %backend -Os -emit=llvm -o - %s | %FileCheck %s --check-prefix=OS
// RUN: %backend -O2 -emit=llvm -o - %s | %FileCheck %s --check-prefix=O2
// OZ: define {{.*}}@f({{.*}}) {{.*}}#[[F:[0-9]+]]
// OZ: define {{.*}}@{{"?}}p.init{{"?}}() {{.*}}#[[I:[0-9]+]]
// OS: attributes #{{[0-9]+}} = { {{.*}}optsize
// OS-NOT: minsize
// O2-NOT: optsize
// Two identical functions are not merged (MergeFunctions is off, see no-merge.mlir).
// OZ: define {{.*}}@g(
// OZ-NOT: tail call {{.*}}@f(
// OZ-DAG: attributes #[[F]] = { {{.*}}minsize{{.*}}optsize
// OZ-DAG: attributes #[[I]] = { {{.*}}optnone
module {
  llvm.mlir.global internal @x(0 : i64) : i64
  llvm.func @f(%a: i64) -> i64 {
    %p = llvm.mlir.addressof @x : !llvm.ptr
    %v = llvm.load %p : !llvm.ptr -> i64
    %m = llvm.mul %a, %v : i64
    %r = llvm.add %m, %a : i64
    llvm.store %r, %p : i64, !llvm.ptr
    llvm.return %r : i64
  }
  llvm.func @g(%a: i64) -> i64 {
    %p = llvm.mlir.addressof @x : !llvm.ptr
    %v = llvm.load %p : !llvm.ptr -> i64
    %m = llvm.mul %a, %v : i64
    %r = llvm.add %m, %a : i64
    llvm.store %r, %p : i64, !llvm.ptr
    llvm.return %r : i64
  }
  llvm.func @"p.init"() {
    llvm.return
  }
}
// RUN: %backend -Oz -emit=llvm -o - %S/Inputs/recover-merge.mlir | %FileCheck %s --check-prefix=REC
// Two identical functions that call recover are not merged into a thunk.
// REC: define {{.*}}@"a$1"(
// REC-NEXT: call {{.*}}@llvm.frameaddress
// REC: define {{.*}}@"b$1"(
// REC-NEXT: call {{.*}}@llvm.frameaddress

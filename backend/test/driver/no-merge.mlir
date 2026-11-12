// MergeFunctions is off at -Os/-Oz by default: merging two
// identical defer thunks adds a frame and recover returns nil
// (tests/run/audit/merge_recover_Os.go). -merge-functions turns it on.
// RUN: %backend -O z -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -O z -merge-functions -emit=llvm -o - %s | %FileCheck %s --check-prefix=MERGED
// CHECK-LABEL: define void @"main.a$thunk1"
// CHECK: call void @main.handler(i64 1)
// CHECK-LABEL: define void @"main.b$thunk2"
// CHECK-NOT: call void @"main.a$thunk1"
// MERGED-LABEL: define void @"main.b$thunk2"
// MERGED: tail call void @"main.a$thunk1"
module {
  llvm.func @main.handler(i64)
  llvm.func @"main.a$thunk1"() {
    %c = llvm.mlir.constant(1 : i64) : i64
    llvm.call @main.handler(%c) : (i64) -> ()
    llvm.call @main.handler(%c) : (i64) -> ()
    llvm.call @main.handler(%c) : (i64) -> ()
    llvm.return
  }
  llvm.func @"main.b$thunk2"() {
    %c = llvm.mlir.constant(1 : i64) : i64
    llvm.call @main.handler(%c) : (i64) -> ()
    llvm.call @main.handler(%c) : (i64) -> ()
    llvm.call @main.handler(%c) : (i64) -> ()
    llvm.return
  }
}

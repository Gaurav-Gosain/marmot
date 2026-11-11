// RUN: %marmot-opt --go-open-defers %s | %FileCheck %s
// RUN: %marmot-opt --go-open-defers --go-to-llvm %s | %FileCheck %s --check-prefix=LOWER
module {
  llvm.mlir.global external constant @"type:clo"() : !llvm.array<56 x i8>
  llvm.func @runtime.gorecover(i64) -> !llvm.struct<(ptr, ptr)>
  llvm.func @external(!llvm.ptr)
  llvm.func @unlock(%env: !llvm.ptr) {
    llvm.return
  }
  llvm.func @recovers(%env: !llvm.ptr) {
    %r = go.recover
    llvm.return
  }
  llvm.func @callsGorecover(%env: !llvm.ptr) {
    %z = llvm.mlir.constant(0 : i64) : i64
    %r = llvm.call @runtime.gorecover(%z) : (i64) -> !llvm.struct<(ptr, ptr)>
    llvm.return
  }
  // A wrapper for `defer recovers2()`: the direct callee may recover.
  llvm.func @wrapper(%env: !llvm.ptr) {
    llvm.call @recovers(%env) : (!llvm.ptr) -> ()
    llvm.return
  }

  // CHECK-LABEL: llvm.func @plain
  // CHECK: go.defer_frame ^bb1, ^bb2 norecover
  // LOWER-LABEL: llvm.func @plain
  // LOWER: %[[FR:.*]] = llvm.alloca %{{.*}} x i64
  // LOWER-NOT: rt_setjmp
  // LOWER: llvm.call @runtime.deferpush
  // LOWER: llvm.call @runtime.deferreturn(%[[FR]])
  llvm.func @plain(%m: !llvm.ptr) {
    go.defer_frame ^body, ^rec
  ^body:
    %c = go.make_closure @unlock(%m), @"type:clo" : (!llvm.ptr) -> !llvm.ptr
    go.defer %c
    go.run_defers
    llvm.return
  ^rec:
    llvm.return
  }

  // CHECK-LABEL: llvm.func @withRecover
  // CHECK: go.defer_frame ^bb1, ^bb2{{$}}
  // LOWER-LABEL: llvm.func @withRecover
  // LOWER: llvm.call @rt_setjmp
  llvm.func @withRecover() {
    go.defer_frame ^body, ^rec
  ^body:
    %c = go.make_closure @recovers(), @"type:clo" : () -> !llvm.ptr
    go.defer %c
    go.run_defers
    llvm.return
  ^rec:
    llvm.return
  }

  // CHECK-LABEL: llvm.func @mixed
  // CHECK: go.defer_frame ^bb1, ^bb2{{$}}
  llvm.func @mixed(%m: !llvm.ptr) {
    go.defer_frame ^body, ^rec
  ^body:
    %c = go.make_closure @unlock(%m), @"type:clo" : (!llvm.ptr) -> !llvm.ptr
    go.defer %c
    %d = go.make_closure @callsGorecover(), @"type:clo" : () -> !llvm.ptr
    go.defer %d
    go.run_defers
    llvm.return
  ^rec:
    llvm.return
  }

  // CHECK-LABEL: llvm.func @wrapped
  // CHECK: go.defer_frame ^bb1, ^bb2{{$}}
  // CHECK-LABEL: llvm.func @notWrapped
  // CHECK: go.defer_frame ^bb1, ^bb2 norecover
  llvm.func @wrapped() {
    go.defer_frame ^body, ^rec
  ^body:
    %c = go.make_closure @wrapper(), @"type:clo" : () -> !llvm.ptr
    go.defer %c wrapped
    go.run_defers
    llvm.return
  ^rec:
    llvm.return
  }
  // Without `wrapped`, recover in a callee of the closure stops nothing.
  llvm.func @notWrapped() {
    go.defer_frame ^body, ^rec
  ^body:
    %c = go.make_closure @wrapper(), @"type:clo" : () -> !llvm.ptr
    go.defer %c
    go.run_defers
    llvm.return
  ^rec:
    llvm.return
  }

  // An unknown func value may recover.
  // CHECK-LABEL: llvm.func @unknown
  // CHECK: go.defer_frame ^bb1, ^bb2{{$}}
  llvm.func @unknown(%f: !llvm.ptr) {
    go.defer_frame ^body, ^rec
  ^body:
    go.defer %f
    go.run_defers
    llvm.return
  ^rec:
    llvm.return
  }
}

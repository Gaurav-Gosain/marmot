// RUN: %marmot-opt --go-to-llvm %s | %FileCheck %s
// RUN: %marmot-opt %s | %marmot-opt | %FileCheck %s --check-prefix=ROUND

module {
  // ROUND-LABEL: llvm.func @f
  // ROUND: go.defer_frame ^bb1, ^bb2
  // ROUND: go.defer %arg0 wrapped
  // ROUND: go.run_defers
  // CHECK-LABEL: llvm.func @f
  // CHECK-DAG: %[[RES:.*]] = llvm.alloca %{{.*}} x i64 :
  // CHECK-DAG: %[[REC:.*]] = llvm.alloca %{{.*}} x !llvm.struct<(ptr, ptr, ptr, i8)>
  // CHECK-DAG: %[[FR:.*]] = llvm.alloca %{{.*}} x !llvm.array<32 x i64>
  // CHECK: %[[J:.*]] = llvm.call @rt_setjmp(%[[FR]]) : (!llvm.ptr) -> i32
  // CHECK: %[[RECOV:.*]] = llvm.icmp "ne" %[[J]]
  // CHECK: llvm.cond_br %[[RECOV]] weights([1, 1048576]), ^[[PAD:.*]], ^[[BODY:.*]]
  // CHECK: ^[[PAD]]:
  // CHECK: llvm.call @runtime.deferreturn(%[[FR]])
  // CHECK: llvm.br ^[[RECOVER:.*]]
  // CHECK: ^[[BODY]]:
  // CHECK: llvm.store volatile %{{.*}}, %[[RES]]
  // CHECK: %[[F1:.*]] = llvm.getelementptr %[[REC]][0, 1]
  // CHECK: llvm.store %arg0, %[[F1]]
  // CHECK: %[[F2:.*]] = llvm.getelementptr %[[REC]][0, 2]
  // CHECK: llvm.store %[[FR]], %[[F2]]
  // CHECK: llvm.store %{{.*}}, %{{.*}} : i8, !llvm.ptr
  // CHECK: llvm.call @runtime.deferpush(%[[REC]])
  // CHECK: llvm.call @runtime.deferreturn(%[[FR]])
  // CHECK: ^[[RECOVER]]:
  // CHECK: llvm.load volatile %[[RES]]
  llvm.func @f(%fn: !llvm.ptr) -> i64 {
    %one = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.alloca %one x i64 : (i64) -> !llvm.ptr
    go.defer_frame ^body, ^recover
  ^body:
    %c7 = llvm.mlir.constant(7 : i64) : i64
    llvm.store %c7, %r : i64, !llvm.ptr
    go.defer %fn wrapped
    go.run_defers
    %v = llvm.load %r : !llvm.ptr -> i64
    llvm.return %v : i64
  ^recover:
    %w = llvm.load %r : !llvm.ptr -> i64
    llvm.return %w : i64
  }

  // A defer in a loop gets a heap record.
  // CHECK-LABEL: llvm.func @loop
  // CHECK: ^[[L:.*]](%{{.*}}: i64):
  // CHECK: llvm.call @runtime.mallocConservative(%{{.*}}) : (i64) -> !llvm.ptr
  // CHECK: llvm.call @runtime.deferpush
  llvm.func @loop(%fn: !llvm.ptr, %n: i64) {
    go.defer_frame ^entry, ^recover
  ^entry:
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    llvm.br ^head(%c0 : i64)
  ^head(%i: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^body, ^done
  ^body:
    go.defer %fn
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^head(%i1 : i64)
  ^done:
    go.run_defers
    llvm.return
  ^recover:
    llvm.return
  }

  // ROUND-LABEL: llvm.func @misc
  // ROUND: go.go %arg0
  // ROUND: go.recover
  // ROUND: go.panic
  // CHECK-LABEL: llvm.func @misc
  // CHECK-SAME: no_inline
  // CHECK: llvm.call @runtime.newproc(%arg0)
  // CHECK: %[[FP:.*]] = llvm.call_intrinsic "llvm.frameaddress.p0"(%{{.*}}) : (i32) -> !llvm.ptr
  // CHECK: %[[FPI:.*]] = llvm.ptrtoint %[[FP]] : !llvm.ptr to i64
  // CHECK: %[[R:.*]] = llvm.call @runtime.gorecover(%[[FPI]])
  // CHECK: llvm.call @runtime.gopanic(%[[R]])
  // CHECK-NEXT: llvm.unreachable
  // CHECK: llvm.func @rt_setjmp(!llvm.ptr) -> i32 attributes {passthrough = ["returns_twice"]}
  // CHECK: llvm.func @runtime.gopanic(!llvm.struct<(ptr, ptr)>) attributes {passthrough = ["noreturn"]}
  llvm.func @misc(%fn: !llvm.ptr) {
    go.go %fn
    %r = go.recover
    go.panic %r
  }
}

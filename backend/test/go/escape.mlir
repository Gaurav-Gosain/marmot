// RUN: %marmot-opt --go-escape %s | %FileCheck %s
// RUN: %marmot-opt --go-escape --go-to-llvm %s | %FileCheck %s --check-prefix=LOWER
!slice = !llvm.struct<(ptr, i64, i64)>
module {
  llvm.mlir.global external constant @"type:int"() : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:clo"() : !llvm.array<56 x i8>
  llvm.mlir.global internal @sink() : !llvm.ptr {
    %z = llvm.mlir.zero : !llvm.ptr
    llvm.return %z : !llvm.ptr
  }
  llvm.func @external(!llvm.ptr)
  llvm.func @external_nocap(!llvm.ptr {llvm.nocapture})
  llvm.func @body(!llvm.ptr)

  // Loads, stores into it, GEPs and nil checks do not escape.
  // CHECK-LABEL: llvm.func @local
  // CHECK: go.new @"type:int", !llvm.struct<(i64, i64)> stack
  // LOWER-LABEL: llvm.func @local
  // LOWER: %[[P:.*]] = llvm.alloca %{{.*}} x !llvm.struct<(i64, i64)>
  // LOWER: %[[Z:.*]] = llvm.mlir.zero : !llvm.struct<(i64, i64)>
  // LOWER: llvm.store %[[Z]], %[[P]]
  // LOWER-NOT: runtime.newobject
  llvm.func @local(%x: i64) -> i64 {
    %p = go.new @"type:int", !llvm.struct<(i64, i64)>
    go.nil_check %p
    %f = llvm.getelementptr %p[0, 1] : (!llvm.ptr) -> !llvm.ptr, !llvm.struct<(i64, i64)>
    llvm.store %x, %f : i64, !llvm.ptr
    %v = llvm.load %f : !llvm.ptr -> i64
    llvm.return %v : i64
  }

  // Returned, stored, or passed to an unknown function: escapes.
  // CHECK-LABEL: llvm.func @escapes
  // LOWER-LABEL: llvm.func @escapes
  // CHECK: go.new @"type:int", i64{{$}}
  // CHECK: go.new @"type:int", i64{{$}}
  // CHECK: go.new @"type:int", i64{{$}}
  // CHECK: go.new @"type:int", i64 stack
  llvm.func @escapes() -> !llvm.ptr {
    %a = go.new @"type:int", i64
    %b = go.new @"type:int", i64
    %s = llvm.mlir.addressof @sink : !llvm.ptr
    llvm.store %b, %s : !llvm.ptr, !llvm.ptr
    %c = go.new @"type:int", i64
    llvm.call @external(%c) : (!llvm.ptr) -> ()
    %d = go.new @"type:int", i64
    llvm.call @external_nocap(%d) : (!llvm.ptr) -> ()
    llvm.return %a : !llvm.ptr
  }

  // Interprocedural: @reads does not let its parameter escape, @keeps does.
  // CHECK-LABEL: llvm.func @reads(%arg0: !llvm.ptr {go.noescape})
  llvm.func @reads(%p: !llvm.ptr) -> i64 {
    %v = llvm.load %p : !llvm.ptr -> i64
    llvm.return %v : i64
  }
  // CHECK-LABEL: llvm.func @keeps(%arg0: !llvm.ptr) {
  llvm.func @keeps(%p: !llvm.ptr) {
    %s = llvm.mlir.addressof @sink : !llvm.ptr
    llvm.store %p, %s : !llvm.ptr, !llvm.ptr
    llvm.return
  }
  // Recursion: the fixpoint keeps no-escape when only recursive calls use it.
  // CHECK-LABEL: llvm.func @rec(%arg0: !llvm.ptr {go.noescape}, %arg1: i64)
  llvm.func @rec(%p: !llvm.ptr, %n: i64) -> i64 {
    %z = llvm.mlir.constant(0 : i64) : i64
    %done = llvm.icmp "eq" %n, %z : i64
    llvm.cond_br %done, ^base, ^step
  ^base:
    %v = llvm.load %p : !llvm.ptr -> i64
    llvm.return %v : i64
  ^step:
    %one = llvm.mlir.constant(1 : i64) : i64
    %m = llvm.sub %n, %one : i64
    %r = llvm.call @rec(%p, %m) : (!llvm.ptr, i64) -> i64
    llvm.return %r : i64
  }
  // Pointer equality is fine for stack allocation but not for a summary.
  // CHECK-LABEL: llvm.func @cmp(%arg0: !llvm.ptr, %arg1: !llvm.ptr)
  llvm.func @cmp(%p: !llvm.ptr, %q: !llvm.ptr) -> i1 {
    %e = llvm.icmp "eq" %p, %q : !llvm.ptr
    llvm.return %e : i1
  }
  // CHECK-LABEL: llvm.func @caller
  // CHECK: go.new @"type:int", i64 stack
  // CHECK: go.new @"type:int", i64{{$}}
  // CHECK: go.new @"type:int", i64 stack
  llvm.func @caller() -> i64 {
    %a = go.new @"type:int", i64
    %v = llvm.call @reads(%a) : (!llvm.ptr) -> i64
    %b = go.new @"type:int", i64
    llvm.call @keeps(%b) : (!llvm.ptr) -> ()
    %c = go.new @"type:int", i64
    %n = llvm.mlir.constant(3 : i64) : i64
    %w = llvm.call @rec(%c, %n) : (!llvm.ptr, i64) -> i64
    %r = llvm.add %v, %w : i64
    llvm.return %r : i64
  }

  // In a loop: allowed when nothing carries the pointer to the next
  // iteration, not allowed through a block argument.
  // CHECK-LABEL: llvm.func @loop
  // CHECK: go.new @"type:int", i64 stack
  // CHECK: go.new @"type:int", i64{{$}}
  llvm.func @loop(%n: i64) -> i64 {
    %z = llvm.mlir.constant(0 : i64) : i64
    %null = llvm.mlir.zero : !llvm.ptr
    llvm.br ^head(%z, %null : i64, !llvm.ptr)
  ^head(%i: i64, %prev: !llvm.ptr):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^body, ^done
  ^body:
    %t = go.new @"type:int", i64
    llvm.store %i, %t : i64, !llvm.ptr
    %u = go.new @"type:int", i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %i1 = llvm.add %i, %one : i64
    llvm.br ^head(%i1, %u : i64, !llvm.ptr)
  ^done:
    llvm.return %i : i64
  }

  // Constant make_slice, box behind an interface used locally, closure
  // deferred outside a loop.
  // CHECK-LABEL: llvm.func @misc
  // CHECK: go.make_slice @"type:int", i64(%{{.*}}, %{{.*}}) stack : i64
  // CHECK: go.make_slice @"type:int", i64(%arg0, %arg0) : i64
  // CHECK: go.box @"type:int", %arg0 stack : i64
  // CHECK: go.make_closure @body(%{{.*}}), @"type:clo" stack
  // LOWER-LABEL: llvm.func @misc
  // LOWER: llvm.alloca %{{.*}} x !llvm.array<4 x i64>
  llvm.func @misc(%n: i64) -> i64 {
    %c4 = llvm.mlir.constant(4 : i64) : i64
    %s = go.make_slice @"type:int", i64(%c4, %c4) : i64
    %d = go.make_slice @"type:int", i64(%n, %n) : i64
    %p = llvm.extractvalue %s[0] : !slice
    %a = go.index %p[%n] len %c4, i64 : i64
    %v = llvm.load %a : !llvm.ptr -> i64
    %bx = go.box @"type:int", %n : i64
    %e = go.make_iface @"type:int", %bx
    %y, %ok = go.type_assert %e, @"type:int" -> @"type:int" commaok
    go.defer_frame ^body, ^rec
  ^body:
    %cl = go.make_closure @body(%n), @"type:clo" : (i64) -> !llvm.ptr
    go.defer %cl
    go.run_defers
    llvm.return %v : i64
  ^rec:
    llvm.return %v : i64
  }
}


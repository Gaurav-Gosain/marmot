// End to end: defer, panic, recover, go against a fake runtime that follows
// the protocol of lib/src/runtime/panic.go with real setjmp/longjmp.
// RUN: %clang -c %S/Inputs/fakert.c -o %t.rt.o
// RUN: %clang -c %S/Inputs/fakert_defer.c -o %t.rtd.o
// RUN: %clang -c %S/Inputs/fakert_jmp.S -o %t.jmp.o
// RUN: %backend -O2 -o %t.o %s
// RUN: %clang %t.o %t.rt.o %t.rtd.o %t.jmp.o -o %t.exe
// RUN: %t.exe 3 | %FileCheck %s --check-prefix=NOPANIC
// RUN: %t.exe 9 | %FileCheck %s --check-prefix=RECOVERED
// RUN: %not %t.exe 100 | %FileCheck %s --check-prefix=FATAL
// RUN: %backend -O0 -o %t.0.o %s
// RUN: %clang %t.0.o %t.rt.o %t.rtd.o %t.jmp.o -o %t.0.exe
// RUN: %t.0.exe 3 | %FileCheck %s --check-prefix=NOPANIC
// RUN: %t.0.exe 9 | %FileCheck %s --check-prefix=RECOVERED
// NOPANIC: result 1006
// RECOVERED: result 26009
// FATAL: panic: 100
// RUN: %t.exe 50 | %FileCheck %s --check-prefix=THROUGH
// RUN: %backend -O2 -emit=mlir -o - %s | %FileCheck %s --check-prefix=IR
// A panic passes through @inner (defer without recover, no setjmp) and is
// recovered in @outer: inc ran (+1), recoverer ran (+100), outer returns 7.
// counter = 1 (go inc) + 1 + 100 = 102 -> 7 + 102*250 = 25507.
// THROUGH: result 25507
// IR-LABEL: llvm.func @inner
// IR-NOT: rt_setjmp
// IR: llvm.call @runtime.deferpush
// IR-LABEL: llvm.func @outer
// IR: rt_setjmp
module {
  llvm.mlir.global external constant @"type:closure"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  llvm.mlir.global internal @counter(0 : i64) : i64

  // func() { counter++ }
  llvm.func @inc(%env: !llvm.ptr) {
    %c = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %c : !llvm.ptr -> i64
    %one = llvm.mlir.constant(1 : i64) : i64
    %v1 = llvm.add %v, %one : i64
    llvm.store %v1, %c : i64, !llvm.ptr
    llvm.return
  }

  // func() { if recover() != nil { counter += 100 } }
  llvm.func @recoverer(%env: !llvm.ptr) {
    %r = go.recover
    %t = llvm.extractvalue %r[0] : !llvm.struct<(ptr, ptr)>
    %z = llvm.mlir.zero : !llvm.ptr
    %got = llvm.icmp "ne" %t, %z : !llvm.ptr
    llvm.cond_br %got, ^yes, ^no
  ^yes:
    %c = llvm.mlir.addressof @counter : !llvm.ptr
    %v = llvm.load %c : !llvm.ptr -> i64
    %h = llvm.mlir.constant(100 : i64) : i64
    %v1 = llvm.add %v, %h : i64
    llvm.store %v1, %c : i64, !llvm.ptr
    llvm.br ^no
  ^no:
    llvm.return
  }

  // func f(k int) (r int) {
  //   defer recoverer()
  //   for i := 0; i < 3; i++ { defer inc() }
  //   r = k
  //   if k > 5 { panic(k) }
  //   r = r * 2
  //   return
  // }
  // r is not captured by any closure: the Recover block must still see r = k.
  llvm.func @f(%k: i64) -> i64 {
    %one = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.alloca %one x i64 : (i64) -> !llvm.ptr
    %zero = llvm.mlir.constant(0 : i64) : i64
    llvm.store %zero, %r : i64, !llvm.ptr
    go.defer_frame ^body, ^recover
  ^body:
    %rc = go.make_closure @recoverer(), @"type:closure" : () -> !llvm.ptr
    go.defer %rc
    llvm.br ^head(%zero : i64)
  ^head(%i: i64):
    %c3 = llvm.mlir.constant(3 : i64) : i64
    %more = llvm.icmp "slt" %i, %c3 : i64
    llvm.cond_br %more, ^loop, ^after
  ^loop:
    %ic = go.make_closure @inc(), @"type:closure" : () -> !llvm.ptr
    go.defer %ic
    %i1 = llvm.add %i, %one : i64
    llvm.br ^head(%i1 : i64)
  ^after:
    llvm.store %k, %r : i64, !llvm.ptr
    %c5 = llvm.mlir.constant(5 : i64) : i64
    %big = llvm.icmp "sgt" %k, %c5 : i64
    llvm.cond_br %big, ^panic, ^ret
  ^panic:
    %c99 = llvm.mlir.constant(99 : i64) : i64
    %pk = llvm.icmp "eq" %k, %c99 : i64
    %d = llvm.inttoptr %k : i64 to !llvm.ptr
    %t = llvm.mlir.addressof @"type:closure" : !llvm.ptr
    %p0 = llvm.mlir.poison : !llvm.struct<(ptr, ptr)>
    %p1 = llvm.insertvalue %t, %p0[0] : !llvm.struct<(ptr, ptr)>
    %p2 = llvm.insertvalue %d, %p1[1] : !llvm.struct<(ptr, ptr)>
    go.panic %p2
  ^ret:
    %v = llvm.load %r : !llvm.ptr -> i64
    %two = llvm.mlir.constant(2 : i64) : i64
    %v2 = llvm.mul %v, %two : i64
    llvm.store %v2, %r : i64, !llvm.ptr
    go.run_defers
    %out = llvm.load %r : !llvm.ptr -> i64
    llvm.return %out : i64
  ^recover:
    %rv = llvm.load %r : !llvm.ptr -> i64
    llvm.return %rv : i64
  }

  // Panics that are not recovered: no defer frame here.
  llvm.func @g(%k: i64) -> i64 {
    %d = llvm.inttoptr %k : i64 to !llvm.ptr
    %t = llvm.mlir.addressof @"type:closure" : !llvm.ptr
    %p0 = llvm.mlir.poison : !llvm.struct<(ptr, ptr)>
    %p1 = llvm.insertvalue %t, %p0[0] : !llvm.struct<(ptr, ptr)>
    %p2 = llvm.insertvalue %d, %p1[1] : !llvm.struct<(ptr, ptr)>
    go.panic %p2
  }

  // func inner(k int) { defer inc(); panic(k) }
  llvm.func @inner(%k: i64) {
    go.defer_frame ^body, ^rec
  ^body:
    %ic = go.make_closure @inc(), @"type:closure" : () -> !llvm.ptr
    go.defer %ic
    %d = llvm.inttoptr %k : i64 to !llvm.ptr
    %t = llvm.mlir.addressof @"type:closure" : !llvm.ptr
    %p0 = llvm.mlir.poison : !llvm.struct<(ptr, ptr)>
    %p1 = llvm.insertvalue %t, %p0[0] : !llvm.struct<(ptr, ptr)>
    %p2 = llvm.insertvalue %d, %p1[1] : !llvm.struct<(ptr, ptr)>
    go.panic %p2
  ^rec:
    llvm.return
  }

  // func outer(k int) (r int) { defer recoverer(); r = 7; inner(k); return 0 }
  llvm.func @outer(%k: i64) -> i64 {
    %one = llvm.mlir.constant(1 : i64) : i64
    %r = llvm.alloca %one x i64 : (i64) -> !llvm.ptr
    go.defer_frame ^body, ^rec
  ^body:
    %rc = go.make_closure @recoverer(), @"type:closure" : () -> !llvm.ptr
    go.defer %rc
    %c7 = llvm.mlir.constant(7 : i64) : i64
    llvm.store %c7, %r : i64, !llvm.ptr
    llvm.call @inner(%k) : (i64) -> ()
    %z = llvm.mlir.constant(0 : i64) : i64
    llvm.store %z, %r : i64, !llvm.ptr
    go.run_defers
    %v = llvm.load %r : !llvm.ptr -> i64
    llvm.return %v : i64
  ^rec:
    %w = llvm.load %r : !llvm.ptr -> i64
    llvm.return %w : i64
  }

  // test_main(k): go inc(); r := f(k) (or g(k) for k == 100); return r + counter*1000/4
  llvm.func @test_main(%k: i64) -> i64 {
    %ic = go.make_closure @inc(), @"type:closure" : () -> !llvm.ptr
    go.go %ic
    %c100 = llvm.mlir.constant(100 : i64) : i64
    %c50 = llvm.mlir.constant(50 : i64) : i64
    %isg = llvm.icmp "eq" %k, %c100 : i64
    %iso = llvm.icmp "eq" %k, %c50 : i64
    llvm.cond_br %iso, ^o, ^notO
  ^o:
    %ro = llvm.call @outer(%k) : (i64) -> i64
    llvm.br ^count(%ro : i64)
  ^notO:
    llvm.cond_br %isg, ^g, ^f
  ^g:
    %rg = llvm.call @g(%k) : (i64) -> i64
    llvm.return %rg : i64
  ^f:
    %rf = llvm.call @f(%k) : (i64) -> i64
    llvm.br ^count(%rf : i64)
  ^count(%r: i64):
    %c = llvm.mlir.addressof @counter : !llvm.ptr
    %n = llvm.load %c : !llvm.ptr -> i64
    %m = llvm.mlir.constant(250 : i64) : i64
    %n2 = llvm.mul %n, %m : i64
    %s = llvm.add %r, %n2 : i64
    llvm.return %s : i64
  }
}

// marmot.card(slot, on) becomes a flag test and a call of the stub for the
// register holding the slot; no call of marmot.card remains.
// RUN: %backend -O2 -target linux/arm64 -emit=asm -o - %s | %FileCheck %s --check-prefix=ARM64
// RUN: %backend -O2 -target linux/amd64 -emit=asm -o - %s | %FileCheck %s --check-prefix=AMD64
// ARM64-LABEL: main.store:
// ARM64: ldrb w[[F:[0-9]+]]
// ARM64-NOT: cset
// ARM64: cbz w[[F]],
// ARM64-NEXT: bl rt_card_x{{([0-9]|1[0-5]|19|2[0-8])$}}
// ARM64-NOT: marmot.card
// AMD64-LABEL: main.store:
// AMD64-NOT: setne
// AMD64: testb %{{[a-z0-9]+}}, %{{[a-z0-9]+}}
// AMD64-NEXT: je
// AMD64-NEXT: callq rt_card_{{(r[a-ds][xi]|r[sd]i|rbp|r[89]|r1[0-5])$}}
// AMD64-NOT: marmot.card
module {
  llvm.func @"marmot.card"(!llvm.ptr, i1)
  llvm.mlir.global external @rt_gc_marking() : i8
  llvm.func @"main.store"(%p: !llvm.ptr, %v: !llvm.ptr) {
    %g = llvm.mlir.addressof @rt_gc_marking : !llvm.ptr
    %f = llvm.load volatile %g : !llvm.ptr -> i8
    %z = llvm.mlir.constant(0 : i8) : i8
    %on = llvm.icmp "ne" %f, %z : i8
    llvm.store %v, %p : !llvm.ptr, !llvm.ptr
    llvm.call @"marmot.card"(%p, %on) : (!llvm.ptr, i1) -> ()
    llvm.return
  }
}

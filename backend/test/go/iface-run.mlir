// End to end: interface calls and type assertions against a fake runtime.
// RUN: %clang -c %S/Inputs/fakert.c -o %t.rt.o
// RUN: %clang -c %S/Inputs/fakert_iface.c -o %t.rti.o
// RUN: %backend -O2 -o %t.o %s
// RUN: %clang %t.o %t.rt.o %t.rti.o -o %t.exe
// RUN: %t.exe 0 | %FileCheck %s --check-prefix=K0
// RUN: %not %t.exe 1 | %FileCheck %s --check-prefix=K1
// RUN: %t.exe 2 | %FileCheck %s --check-prefix=K2
// RUN: %not %t.exe 3 | %FileCheck %s --check-prefix=K3
// RUN: %backend -O0 -o %t.0.o %s
// RUN: %clang %t.0.o %t.rt.o %t.rti.o -o %t.0.exe
// RUN: %t.0.exe 0 | %FileCheck %s --check-prefix=K0
// RUN: %not %t.0.exe 1 | %FileCheck %s --check-prefix=K1
// RUN: %t.0.exe 2 | %FileCheck %s --check-prefix=K2
// RUN: %not %t.0.exe 3 | %FileCheck %s --check-prefix=K3
// K0: result 1000
// K1: panic: interface conversion: have *main.T, want other
// K2: result 2007
// K3: panic: nil pointer dereference
module {
  llvm.mlir.global external constant @"type:main.I"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:*main.T"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:main.T"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:int"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  llvm.mlir.global external constant @"type:any"(dense<0> : tensor<56xi8>) : !llvm.array<56 x i8>
  // itab layout of lib/src/runtime/type.go: {inter, typ, hash, _, fun[1]}.
  llvm.mlir.global external constant @test_itab() : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)> {
    %0 = llvm.mlir.zero : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
    %i = llvm.mlir.addressof @"type:main.I" : !llvm.ptr
    %t = llvm.mlir.addressof @"type:*main.T" : !llvm.ptr
    %f = llvm.mlir.addressof @"main.(*T).Get" : !llvm.ptr
    %1 = llvm.insertvalue %i, %0[0] : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
    %2 = llvm.insertvalue %t, %1[1] : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
    %3 = llvm.insertvalue %f, %2[4, 0] : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
    llvm.return %3 : !llvm.struct<(ptr, ptr, i32, i32, array<1 x ptr>)>
  }

  // func (t *T) Get() int { return t.x }
  llvm.func @"main.(*T).Get"(%t: !llvm.ptr) -> i64 {
    %v = llvm.load %t : !llvm.ptr -> i64
    llvm.return %v : i64
  }

  llvm.func @test_main(%k: i64) -> i64 {
    %c1000 = llvm.mlir.constant(1000 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %c2 = llvm.mlir.constant(2 : i64) : i64
    %c3 = llvm.mlir.constant(3 : i64) : i64
    %p = go.new @"type:main.T", i64
    %x = llvm.add %c1000, %k : i64
    llvm.store %x, %p : i64, !llvm.ptr
    %i = go.make_iface @test_itab, %p
    %r = go.iface_call %i[0]() : () -> i64
    // Assert back from I to *T (reads the itab type word).
    %d = go.type_assert %i, @"type:main.I" -> @"type:*main.T" from_iface
    %v = llvm.load %d : !llvm.ptr -> i64
    %sum = llvm.add %r, %v : i64
    // The eface form: e.(*T) succeeds, e.(int) comma-ok fails.
    %e = go.make_iface @"type:*main.T", %p
    %d2, %ok2 = go.type_assert %e, @"type:any" -> @"type:int" commaok
    %bad = llvm.zext %ok2 : i1 to i64
    %s1 = llvm.add %sum, %bad : i64
    %is1 = llvm.icmp "eq" %k, %c1 : i64
    %is2 = llvm.icmp "eq" %k, %c2 : i64
    %is3 = llvm.icmp "eq" %k, %c3 : i64
    llvm.cond_br %is1, ^panic_assert, ^next
  ^panic_assert:
    %no = go.type_assert %e, @"type:any" -> @"type:int"
    llvm.return %c1 : i64
  ^next:
    llvm.cond_br %is2, ^to_iface, ^next2
  ^to_iface:
    %j = go.type_assert %e, @"type:any" -> @"type:main.I" to_iface
    %r2 = go.iface_call %j[0]() : () -> i64
    %j2, %okj = go.type_assert %e, @"type:any" -> @"type:main.I" to_iface commaok
    %okx = llvm.zext %okj : i1 to i64
    %t1 = llvm.add %s1, %r2 : i64
    %t2 = llvm.add %t1, %okx : i64
    %t3 = llvm.sub %t2, %c1000 : i64
    llvm.return %t3 : i64
  ^next2:
    llvm.cond_br %is3, ^nilcall, ^done
  ^nilcall:
    %z = llvm.mlir.zero : !llvm.struct<(ptr, ptr)>
    %r3 = go.iface_call %z[0]() : () -> i64
    llvm.return %r3 : i64
  ^done:
    %u = llvm.sub %s1, %c1000 : i64
    llvm.return %u : i64
  }
}

// Runtime bitcode import: small pure functions inline, functions with
// internal state stay calls, runtime globals are never defined in the output.
// RUN: %clang -O2 -c -emit-llvm %S/Inputs/rt.c -o %t.rt.bc
// RUN: %backend -rt-bitcode %t.rt.bc -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -rt-bitcode %t.rt.bc -o %t.o %s
// RUN: %llvm-nm %t.o | %FileCheck %s --check-prefix=NM
// At -O0 the runtime bitcode is not linked at all.
// RUN: %backend -O0 -rt-bitcode %t.rt.bc -emit=llvm -o - %s | %FileCheck %s --check-prefix=O0
// CHECK-NOT: define {{.*}}@rt_
// CHECK-NOT: @counter
// CHECK: @rt_state_ext = external {{.*}}global
// CHECK-NOT: define {{.*}}@rt_
// CHECK-LABEL: define {{.*}}i64 @use(
// CHECK-NOT: call {{.*}}@rt_add
// CHECK-NOT: call {{.*}}@rt_twice
// CHECK: call {{.*}}@rt_count()
// CHECK: load {{.*}}@rt_state_ext
// CHECK: call {{.*}}@rt_slow(
// CHECK-NOT: define {{.*}}@rt_
// NM-NOT: {{ [TDdtb] _?rt_}}
// NM-NOT: counter
// O0: call i64 @rt_add
module {
  llvm.func @rt_add(i64, i64) -> i64
  llvm.func @rt_count() -> i64
  llvm.func @rt_ext() -> i64
  llvm.func @rt_slow(i64) -> i64
  llvm.func @rt_twice(i64) -> i64
  llvm.func @use(%a: i64) -> i64 {
    %0 = llvm.call @rt_add(%a, %a) : (i64, i64) -> i64
    %1 = llvm.call @rt_twice(%0) : (i64) -> i64
    %2 = llvm.call @rt_count() : () -> i64
    %3 = llvm.call @rt_ext() : () -> i64
    %4 = llvm.call @rt_slow(%1) : (i64) -> i64
    %5 = llvm.add %2, %3 : i64
    %6 = llvm.add %5, %4 : i64
    llvm.return %6 : i64
  }
}

// PGO round trip: instrument, run, merge, optimize with the profile.
// RUN: rm -rf %t.d && mkdir -p %t.d
// RUN: %backend -O2 -pgo-gen=%t.d/p.profraw -o %t.gen.o %s
// RUN: %clang -fprofile-generate %t.gen.o -o %t.gen
// RUN: %t.gen
// RUN: %llvm-profdata merge -o %t.profdata %t.d/p.profraw
// RUN: %backend -O2 -pgo=%t.profdata -emit=llvm -o - %s | %FileCheck %s
// RUN: %not %backend -pgo=%t.profdata -pgo-gen -o %t.x.o %s 2>&1 | %FileCheck %s --check-prefix=EXCL
// The hot path of @classify gets the profile's branch weights.
// CHECK: define {{.*}}@classify({{.*}}!prof
// CHECK: !{!"function_entry_count", i64 1000}
// EXCL: -pgo and -pgo-gen are exclusive
module {
  llvm.func @classify(%x: i64) -> i64 attributes {passthrough = ["noinline"]} {
    %c7 = llvm.mlir.constant(7 : i64) : i64
    %r = llvm.urem %x, %c7 : i64
    %z = llvm.mlir.constant(0 : i64) : i64
    %rare = llvm.icmp "eq" %r, %z : i64
    llvm.cond_br %rare, ^a, ^b
  ^a:
    %m = llvm.mul %x, %x : i64
    llvm.return %m : i64
  ^b:
    %s = llvm.add %x, %r : i64
    llvm.return %s : i64
  }
  llvm.func @main() -> i32 {
    %c0 = llvm.mlir.constant(0 : i64) : i64
    %c1 = llvm.mlir.constant(1 : i64) : i64
    %n = llvm.mlir.constant(1000 : i64) : i64
    llvm.br ^h(%c0, %c0 : i64, i64)
  ^h(%i: i64, %acc: i64):
    %more = llvm.icmp "slt" %i, %n : i64
    llvm.cond_br %more, ^b, ^d
  ^b:
    %v = llvm.call @classify(%i) : (i64) -> i64
    %a2 = llvm.add %acc, %v : i64
    %i1 = llvm.add %i, %c1 : i64
    llvm.br ^h(%i1, %a2 : i64, i64)
  ^d:
    %z = llvm.mlir.constant(0 : i32) : i32
    llvm.return %z : i32
  }
}

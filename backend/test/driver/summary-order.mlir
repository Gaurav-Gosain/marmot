// A module summary lists what a definition references in the order of the
// IR, also above 16 references (a pointer keyed set listed them in address
// order, which changed from run to run and with it the dev imports).
// RUN: rm -rf %t.cache && mkdir -p %t.d && cp %s %t.d/a.mlir
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -cache-dir %t.cache
// RUN: cat %t.cache/*/*.sum | %FileCheck %s
// CHECK: D{{.}}fg{{..}}{{.}}f{{$}}
// CHECK: R{{.}}g0{{$}}
// CHECK-NEXT: R{{.}}g1{{$}}
// CHECK-NEXT: R{{.}}g2{{$}}
// CHECK-NEXT: R{{.}}g3{{$}}
// CHECK-NEXT: R{{.}}g4{{$}}
// CHECK-NEXT: R{{.}}g5{{$}}
// CHECK-NEXT: R{{.}}g6{{$}}
// CHECK-NEXT: R{{.}}g7{{$}}
// CHECK-NEXT: R{{.}}g8{{$}}
// CHECK-NEXT: R{{.}}g9{{$}}
// CHECK-NEXT: R{{.}}g10{{$}}
// CHECK-NEXT: R{{.}}g11{{$}}
// CHECK-NEXT: R{{.}}g12{{$}}
// CHECK-NEXT: R{{.}}g13{{$}}
// CHECK-NEXT: R{{.}}g14{{$}}
// CHECK-NEXT: R{{.}}g15{{$}}
// CHECK-NEXT: R{{.}}g16{{$}}
// CHECK-NEXT: R{{.}}g17{{$}}
// CHECK-NEXT: R{{.}}g18{{$}}
// CHECK-NEXT: R{{.}}g19{{$}}
module {
  llvm.func @g0()
  llvm.func @g1()
  llvm.func @g2()
  llvm.func @g3()
  llvm.func @g4()
  llvm.func @g5()
  llvm.func @g6()
  llvm.func @g7()
  llvm.func @g8()
  llvm.func @g9()
  llvm.func @g10()
  llvm.func @g11()
  llvm.func @g12()
  llvm.func @g13()
  llvm.func @g14()
  llvm.func @g15()
  llvm.func @g16()
  llvm.func @g17()
  llvm.func @g18()
  llvm.func @g19()
  llvm.func @f() {
    llvm.call @g0() : () -> ()
    llvm.call @g1() : () -> ()
    llvm.call @g2() : () -> ()
    llvm.call @g3() : () -> ()
    llvm.call @g4() : () -> ()
    llvm.call @g5() : () -> ()
    llvm.call @g6() : () -> ()
    llvm.call @g7() : () -> ()
    llvm.call @g8() : () -> ()
    llvm.call @g9() : () -> ()
    llvm.call @g10() : () -> ()
    llvm.call @g11() : () -> ()
    llvm.call @g12() : () -> ()
    llvm.call @g13() : () -> ()
    llvm.call @g14() : () -> ()
    llvm.call @g15() : () -> ()
    llvm.call @g16() : () -> ()
    llvm.call @g17() : () -> ()
    llvm.call @g18() : () -> ()
    llvm.call @g19() : () -> ()
    llvm.return
  }
}

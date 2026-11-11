// Line tables from MLIR locations (-g 1, the default): a DISubprogram per
// function and a line for each instruction, in the object's DWARF.
// RUN: %backend -O2 -emit=llvm -o - %s | %FileCheck %s
// RUN: %backend -O2 -o %t.o %s
// RUN: %llvm-dwarfdump --debug-line %t.o | %FileCheck %s --check-prefix=LINES
// RUN: %backend -O2 -g 0 -emit=llvm -o - %s | %FileCheck %s --check-prefix=NONE
// CHECK: define {{.*}}@{{"?}}main.add{{"?}}({{.*}}!dbg ![[SP:[0-9]+]]
// CHECK: !DICompileUnit({{.*}}emissionKind: LineTablesOnly
// CHECK: ![[SP]] = distinct !DISubprogram(name: "main.add"
// LINES: name: "main.go"
// LINES: 0x0000000000000000 7 9
// LINES: 0x{{[0-9a-f]+}} 8 11
// NONE-NOT: !dbg
// Without frontend locations (only positions in the .mlir text) nothing is emitted.
// RUN: %backend -O2 -emit=llvm -o - %S/hello-run.mlir | %FileCheck %s --check-prefix=NOLOC
// NOLOC-NOT: DICompileUnit
module {
  llvm.func @"main.add"(%a: i64, %b: i64) -> i64 {
    %s = llvm.add %a, %b : i64 loc("/src/p/main.go":7:9)
    %c = llvm.mlir.constant(3 : i64) : i64 loc("/src/p/main.go":8:2)
    %m = llvm.mul %s, %c : i64 loc("/src/p/main.go":8:11)
    llvm.return %m : i64 loc("/src/p/main.go":9:2)
  } loc("/src/p/main.go":6:6)
} loc("/src/p/main.go":1:1)

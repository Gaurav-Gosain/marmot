// The compact pc table: link, dsymutil, marmot-pcln, decode pc -> line.
// UNSUPPORTED: x86_64
// RUN: %backend -O1 -o %t.o %S/debuginfo.mlir
// RUN: %clang -c %S/Inputs/pcln_main.c -o %t.main.o
// RUN: %clang %t.o %t.main.o -o %t.exe
// RUN: rm -rf %t.dSYM && %dsymutil %t.exe -o %t.dSYM
// RUN: %pcln -o %t.pcln %t.dSYM/Contents/Resources/DWARF/%{basename_t}.exe -base-from %t.exe
// RUN: python3 %S/Inputs/pcln_decode.py %t.pcln $(python3 %S/Inputs/pcln_offsets.py %t.exe main.add 0 4 8) | %FileCheck %s
// CHECK: main.add /src/p/main.go:7
// CHECK-NEXT: main.add /src/p/main.go:8
// CHECK-NEXT: main.add /src/p/main.go:9

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
// C functions (no DWARF lines) get name-only entries that end the Go ranges,
// and -main-pkg names the main package's functions main.X.
// RUN: %pcln -o %t.pcln2 %t.dSYM/Contents/Resources/DWARF/%{basename_t}.exe -base-from %t.exe -main-pkg=example.com/p
// RUN: python3 %S/Inputs/pcln_decode.py %t.pcln2 $(python3 %S/Inputs/pcln_offsets.py %t.exe main 0) $(python3 %S/Inputs/pcln_offsets.py %t.exe example.com/p.helper 0) $(python3 %S/Inputs/pcln_offsets.py %t.exe main.add 0) | %FileCheck %s --check-prefix=SYMS
// SYMS: main{{$}}
// SYMS-NEXT: main.helper{{$}}
// SYMS-NEXT: main.add /src/p/main.go:7

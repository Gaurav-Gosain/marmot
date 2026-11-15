// marmot-pcln -emit-source-map: the DWARF lines of a wasm module as a source map. Read
// back, it gives the lines the DWARF table gives.
// REQUIRES: wasm
// RUN: %backend -O1 -target wasip1/wasm -o %t.o %S/debuginfo.mlir
// RUN: %pcln -emit-source-map %t.o.map %t.o
// RUN: %pcln -o %t.dw.pcln %t.o
// RUN: %pcln -wasm-final -source-map %t.o.map -o %t.sm.pcln %t.o
// RUN: python3 %S/Inputs/pcln_decode.py %t.dw.pcln $(python3 %S/Inputs/srcmap_offsets.py %t.o.map %t.o) | cut -d' ' -f2 > %t.dw.lines
// RUN: python3 %S/Inputs/pcln_decode.py %t.sm.pcln $(python3 %S/Inputs/srcmap_offsets.py %t.o.map %t.o) | cut -d' ' -f2 > %t.sm.lines
// RUN: diff %t.dw.lines %t.sm.lines
// RUN: %FileCheck %s --check-prefix=LINES < %t.sm.lines
// LINES: /src/p/main.go:7

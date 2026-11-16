// marmot-pcln -wasm-final: the table from a final wasm module (after
// wasm-opt), from code section bodies and the name section; no DWARF, no
// lines. Imported functions get no entry; an unnamed body gets the name
// browsers show. Offsets are code section offsets, as for wasm objects.
// RUN: python3 %S/Inputs/wasm_final.py %t.wasm
// RUN: %pcln -wasm-final -o %t.pcln %t.wasm
// RUN: python3 %S/Inputs/pcln_decode.py %t.pcln 0 1 4 5 7 8 11 12 | %FileCheck %s
// CHECK: ?
// CHECK-NEXT: main.adder.func1{{$}}
// CHECK-NEXT: main.adder.func1{{$}}
// CHECK-NEXT: main.main{{$}}
// CHECK-NEXT: main.main{{$}}
// CHECK-NEXT: wasm-function[3]{{$}}
// CHECK-NEXT: wasm-function[3]{{$}}
// CHECK-NEXT: ?
// RUN: printf '\0asm' > %t.bad && %not %pcln -wasm-final -o %t.bad.pcln %t.bad 2>&1 | %FileCheck %s --check-prefix=BAD
// BAD: not a wasm module

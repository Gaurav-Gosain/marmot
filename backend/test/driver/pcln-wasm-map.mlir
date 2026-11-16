// marmot-pcln -wasm-final -source-map: lines from the source map of a final
// module (wasm-opt --output-source-map). Columns are file offsets; a
// segment without a source is skipped (main.main starts with one, and its
// entry line is its first mapped row).
// RUN: python3 %S/Inputs/wasm_final.py %t.wasm %t.map
// RUN: %pcln -wasm-final -source-map %t.map -o %t.pcln %t.wasm
// RUN: python3 %S/Inputs/pcln_decode.py %t.pcln 1 2 3 4 5 6 7 | %FileCheck %s
// CHECK: main.adder.func1 /src/a.go:10
// CHECK-NEXT: main.adder.func1 /src/a.go:10
// CHECK-NEXT: main.adder.func1 /src/a.go:11
// CHECK-NEXT: main.adder.func1 /src/a.go:11
// CHECK-NEXT: main.main /src/b.go:20
// CHECK-NEXT: main.main /src/b.go:20
// CHECK-NEXT: main.main /src/b.go:20
// RUN: %not %pcln -source-map %t.map -o %t.x %t.wasm 2>&1 | %FileCheck %s --check-prefix=NEEDS
// NEEDS: -source-map needs -wasm-final
// RUN: printf '{"version":3}' > %t.bad.map
// RUN: %not %pcln -wasm-final -source-map %t.bad.map -o %t.x %t.wasm 2>&1 | %FileCheck %s --check-prefix=BADMAP
// BADMAP: not a source map

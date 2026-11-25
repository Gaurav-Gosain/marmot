// Fragments of relocatable ELF objects (dev builds, tier 1 objects): in a
// line-tables-only unit LLVM writes no DWARF subprogram for a function without
// inlined calls (every target but Darwin), so the rows of main.add have no
// DWARF function. Its symbol gives the range, so the fragment keeps the rows.
// RUN: rm -rf %t && mkdir -p %t && printf '%S/debuginfo.mlir %t/a.o\n' > %t/jobs
// RUN: %backend -O2 --target=x86_64-unknown-linux-gnu -batch %t/jobs -pcln-frags -cache-dir %t/cache
// RUN: %llvm-dwarfdump --debug-info %t/a.o | %FileCheck %s --check-prefix=DWARF
// DWARF-NOT: DW_TAG_subprogram
// RUN: %FileCheck %s < %t/a.o.pcln
// CHECK: P	/src/p/main.go
// CHECK-NEXT: F	{{[0-9]+}}	main.add
// CHECK-NEXT: R	0	7	0
// CHECK-NEXT: R	{{[0-9]+}}	8	0
// CHECK-NEXT: R	{{[0-9]+}}	9	0

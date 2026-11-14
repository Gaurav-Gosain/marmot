// tier0 x86-64 ELF objects with more than 65280 sections (one per function)
// use extended numbering: the section count and the string table index in
// the null section header, symbol section indices in .symtab_shndx.
// RUN: rm -rf %t.d && mkdir -p %t.d
// RUN: python3 %S/Inputs/many_funcs.py 70000 > %t.d/m.mlir
// RUN: printf '%t.d/m.mlir %t.d/m.o\n' > %t.d/jobs
// RUN: %backend -O0 -tier0 --target=linux/amd64 -batch %t.d/jobs
// RUN: %llvm-readobj --file-headers --sections %t.d/m.o | %FileCheck %s --check-prefix=HDR
// RUN: %llvm-readobj --symbols %t.d/m.o | %FileCheck %s --check-prefix=SYM
// HDR: SectionHeaderCount: 0 (70011)
// HDR: StringTableSectionIndex: 65535 (70010)
// HDR: Name: .symtab_shndx
// SYM: Name: f69999 (
// SYM-NEXT: Value:
// SYM-NEXT: Size:
// SYM-NEXT: Binding: Global
// SYM-NEXT: Type: Function
// SYM-NEXT: Other: 0
// SYM-NEXT: Section: .text (0x11170)

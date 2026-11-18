// marmot-pcln -dwarf-dir writes DWARF line tables from the pc table's rows
// (MARMOT_DWARF=1): one compile unit per Go package, DW_LANG_Go, a
// subprogram per function, prologue_end on the second row of a function.
// The binary passes llvm-dwarfdump --verify after the linker's DWARF is
// replaced by these sections.
// UNSUPPORTED: arm64
// RUN: rm -rf %t.d %t.n
// RUN: %clang --target=x86_64-unknown-linux-gnu -g -O1 -c %S/Inputs/pcln_dwarf.c -o %t.o
// RUN: %clang --target=x86_64-unknown-linux-gnu -nostdlib -static -no-pie %t.o -o %t.exe
// RUN: %pcln -o %t.pcln -dwarf-dir %t.d %t.exe 2>&1 | %FileCheck %s
// CHECK: DWARF {{[0-9]+}} bytes in 6 sections
// RUN: %llvm-objcopy --strip-debug --add-section .debug_info=%t.d/debug_info --add-section .debug_abbrev=%t.d/debug_abbrev --add-section .debug_line=%t.d/debug_line --add-section .debug_str=%t.d/debug_str --add-section .debug_ranges=%t.d/debug_ranges --add-section .debug_aranges=%t.d/debug_aranges %t.exe %t.out
// RUN: %llvm-dwarfdump --verify %t.out | %FileCheck %s --check-prefix=VERIFY
// VERIFY: No errors.
// RUN: %llvm-dwarfdump --debug-info %t.out | %FileCheck %s --check-prefix=INFO
// INFO: DW_TAG_compile_unit
// INFO: DW_AT_producer ("marmot")
// INFO: DW_AT_language (DW_LANG_Go)
// INFO: DW_AT_name ("main")
// INFO: DW_TAG_subprogram
// INFO: DW_AT_name ("main.energy")
// INFO: DW_TAG_compile_unit
// INFO: DW_AT_name ("example.com/m/pkg")
// INFO: DW_AT_name ("example.com/m/pkg.Sum")
// RUN: %llvm-dwarfdump --debug-line %t.out | %FileCheck %s --check-prefix=LINE
// LINE: file_names[ 1]:
// LINE-NEXT: name: "pcln_dwarf.c"
// LINE: prologue_end
// Without subprograms, names come from the symbol table.
// RUN: %pcln -o %t.pcln2 -dwarf-dir %t.n -dwarf-subprograms=false %t.exe
// RUN: %llvm-objcopy --strip-debug --add-section .debug_info=%t.n/debug_info --add-section .debug_abbrev=%t.n/debug_abbrev --add-section .debug_line=%t.n/debug_line --add-section .debug_str=%t.n/debug_str --add-section .debug_ranges=%t.n/debug_ranges --add-section .debug_aranges=%t.n/debug_aranges %t.exe %t.out2
// RUN: %llvm-dwarfdump --verify %t.out2 | %FileCheck %s --check-prefix=VERIFY
// RUN: %llvm-dwarfdump --debug-info %t.out2 | %FileCheck %s --check-prefix=NOSUB
// NOSUB: DW_TAG_compile_unit
// NOSUB-NOT: DW_TAG_subprogram

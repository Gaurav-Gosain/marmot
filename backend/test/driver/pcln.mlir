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
// Version 3: inlined frames (DW_TAG_inlined_subroutine), innermost first.
// RUN: %clang -g -O1 -c %S/Inputs/pcln_inline.c -o %t.inl.o
// RUN: %clang %t.inl.o -o %t.inl.exe
// RUN: rm -rf %t.inl.dSYM && %dsymutil %t.inl.exe -o %t.inl.dSYM
// RUN: %pcln -table-version=3 -o %t.inl.pcln %t.inl.dSYM/Contents/Resources/DWARF/%{basename_t}.inl.exe -base-from %t.inl.exe
// RUN: python3 %S/Inputs/pcln_decode.py %t.inl.pcln --inline $(python3 %S/Inputs/pcln_offsets.py %t.inl.exe outer 0) | %FileCheck %s --check-prefix=INL
// INL: mid {{.*}}pcln_inline.c:8 <- outer {{.*}}pcln_inline.c:13
// INL-NEXT: leaf {{.*}}pcln_inline.c:4 <- mid {{.*}}pcln_inline.c:9 <- outer {{.*}}pcln_inline.c:13
// Fragments (no dsymutil, one link): the backend writes OUT.pcln, the link
// carries a placeholder section, marmot-pcln writes the table into it and
// rehashes the code signature.
// RUN: rm -rf %t.fr && mkdir -p %t.fr && printf '%S/debuginfo.mlir %t.fr/a.o\n' > %t.fr/jobs
// RUN: %backend -O1 -batch %t.fr/jobs -pcln-frags -cache-dir %t.fr/cache
// RUN: test -f %t.fr/a.o.pcln && echo %t.fr/a.o.pcln > %t.fr/frags
// RUN: echo %t.main.o > %t.fr/extra
// RUN: %pcln -frags %t.fr/frags -estimate -extra-objects %t.fr/extra > %t.fr/size
// RUN: head -c $(cat %t.fr/size) /dev/zero > %t.fr/ph
// RUN: %clang %t.fr/a.o %t.main.o -Wl,-sectcreate,__TEXT,__marmot_pcln,%t.fr/ph -o %t.fr/exe
// RUN: %pcln -frags %t.fr/frags -patch %t.fr/exe
// RUN: codesign -v %t.fr/exe
// RUN: %t.fr/exe; test $? -eq 15
// RUN: %llvm-objcopy --dump-section __TEXT,__marmot_pcln=%t.fr/table %t.fr/exe %t.fr/junk
// RUN: python3 %S/Inputs/pcln_decode.py %t.fr/table $(python3 %S/Inputs/pcln_offsets.py %t.fr/exe main.add 0 4 8) | %FileCheck %s --check-prefix=FRAG
// FRAG: main.add /src/p/main.go:7
// FRAG-NEXT: main.add /src/p/main.go:8
// FRAG-NEXT: main.add /src/p/main.go:9
// A placeholder that is too small: exit 3 and the size needed.
// RUN: head -c 8 /dev/zero > %t.fr/ph2
// RUN: %clang %t.fr/a.o %t.main.o -Wl,-sectcreate,__TEXT,__marmot_pcln,%t.fr/ph2 -o %t.fr/exe2
// RUN: %not %pcln -frags %t.fr/frags -patch %t.fr/exe2 2>&1 | %FileCheck %s --check-prefix=SMALL
// SMALL: needs {{[0-9]+}} bytes
// ELF line-tables-only debug info has no DW_TAG_subprogram (LLVM 22, as clang
// -gline-tables-only): rows go to the function symbols. Before, every Linux
// table had names but no lines (runtime.Caller gave "" and 0).
// RUN: %backend -target linux/amd64 -O1 -o %t.elf.o %S/debuginfo.mlir
// RUN: %pcln -o %t.elf.pcln %t.elf.o
// RUN: python3 %S/Inputs/pcln_decode.py %t.elf.pcln 0 | %FileCheck %s --check-prefix=ELF
// ELF: main.add /src/p/main.go:7

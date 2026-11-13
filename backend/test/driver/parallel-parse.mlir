// A large module parses in chunks on spare threads in a tier0 batch: the
// object is the one the serial parse gives, and an error in the last chunk
// is reported at its line in the input (the serial parse reports it).
// UNSUPPORTED: aarch64
// RUN: python3 %S/Inputs/big_module.py %t.mlir
// RUN: printf '%t.mlir %t.a.o\n' > %t.jobs && printf '%t.mlir %t.b.o\n' > %t.jobs1
// RUN: %backend -tier0 -batch %t.jobs
// RUN: MARMOT_PARSE_THREADS=1 %backend -tier0 -batch %t.jobs1
// RUN: cmp %t.a.o %t.b.o
// RUN: %clang %t.a.o -o %t.exe && %t.exe; test $? -eq 42
// Location aliases before the module line: the same object again.
// RUN: python3 %S/Inputs/big_module.py %t.lf.mlir locsfirst
// RUN: printf '%t.lf.mlir %t.d.o\n' > %t.jobs3 && printf '%t.lf.mlir %t.e.o\n' > %t.jobs4
// RUN: %backend -tier0 -batch %t.jobs3
// RUN: MARMOT_PARSE_THREADS=1 %backend -tier0 -batch %t.jobs4
// RUN: cmp %t.d.o %t.e.o && cmp %t.a.o %t.d.o
// RUN: python3 %S/Inputs/big_module.py %t.bad.mlir bad 2> %t.line
// RUN: printf '%t.bad.mlir %t.c.o\n' > %t.jobs2
// RUN: %not %backend -tier0 -batch %t.jobs2 2> %t.err
// RUN: grep -q "bad.mlir:$(cat %t.line):" %t.err
// RUN: test $(grep -c error %t.err) -eq 1

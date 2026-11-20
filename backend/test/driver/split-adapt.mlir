// -split-adapt (-wpo): the split threshold follows the program size, at
// least 2000 instructions. A one-module program of about 3000 instructions
// is not split at -split-large=100000, and is split with -split-adapt. The
// groups do not depend on the job count.
// RUN: rm -rf %t.d && mkdir -p %t.d
// RUN: python3 %S/Inputs/gen_split_adapt.py %t.d/a.mlir 2> %t.want
// RUN: printf '%t.d/a.mlir %t.a.o\n' > %t.jobs
// RUN: %backend -O2 -batch %t.jobs -wpo -split-large=100000
// RUN: test ! -e %t.a.o.parts
// RUN: %backend -O2 -batch %t.jobs -wpo -split-large=100000 -split-adapt -j 1
// RUN: test -f %t.a.o.parts
// RUN: cat $(cat %t.a.o.parts) > %t.j1
// RUN: %clang $(cat %t.a.o.parts) -o %t.exe && %t.exe; test $? -eq $(cat %t.want)
// RUN: %backend -O2 -batch %t.jobs -wpo -split-large=100000 -split-adapt -j 4
// RUN: cat $(cat %t.a.o.parts) > %t.j4
// RUN: cmp %t.j1 %t.j4

#!/usr/bin/env python3
"""Writes a module with one external global: an array of 100000 i8 given
as a go.init string (byte k is k mod 251)."""
import sys

n = 100000
init = "(" + "".join("i%d;" % (k % 251) for k in range(n)) + ")"
with open(sys.argv[1], "w") as f:
    f.write("module {\n")
    f.write('  llvm.mlir.global external @t() {addr_space = 0 : i32, go.init = "%s"} : !llvm.array<%d x i8>\n' % (init, n))
    f.write("}\n")

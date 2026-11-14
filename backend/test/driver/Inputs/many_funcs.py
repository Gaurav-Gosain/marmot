#!/usr/bin/env python3
"""Prints an MLIR module of N functions (argv[1]) and a main calling the last."""
import sys

n = int(sys.argv[1])
print("module {")
for i in range(n):
    print(f"  llvm.func @f{i}(%a: i64) -> i64 {{")
    print(f"    %c = llvm.mlir.constant({i} : i64) : i64")
    print("    %r = llvm.add %a, %c : i64")
    print("    llvm.return %r : i64")
    print("  }")
print("  llvm.func @main() -> i32 {")
print("    %c = llvm.mlir.constant(1 : i64) : i64")
print(f"    %r = llvm.call @f{n - 1}(%c) : (i64) -> i64")
print("    %t = llvm.trunc %r : i64 to i32")
print("    llvm.return %t : i32")
print("  }")
print("}")

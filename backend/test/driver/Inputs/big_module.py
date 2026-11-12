#!/usr/bin/env python3
"""Writes a module of about 3 MB in the shape the frontend prints: type
aliases before the module, location aliases after it, strings and comments
with braces. main returns 42 through a call into the last function.
With "bad", the last function has a type error; prints its line on stderr."""
import sys

out, mode = sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else "ok"
n = 3000
lines = ["// a big module", "!t0 = !llvm.struct<(ptr, i64)>", "module {"]
lines.append('  llvm.mlir.global private constant @".str.0"("{ \\22quoted\\22 } }}") {addr_space = 0 : i32}')
lines.append("  llvm.mlir.global internal @counter(0 : i64) {addr_space = 0 : i32} : i64")
bad_line = 0
loc = 0
for i in range(n):
    lines.append(f"  // f{i} {{ not a region")
    lines.append(f"  llvm.func @f{i}(%a: i64) -> i64 {{")
    for k in range(8):
        lines.append(f"    %c{k} = llvm.mlir.constant({i + k} : i64) : i64 loc(#loc{loc})")
        lines.append(f"    %s{k} = llvm.add %a, %c{k} : i64 loc(#loc{loc + 1})")
        loc += 2
    lines.append("    %v = llvm.mlir.undef : !t0")
    if mode == "bad" and i == n - 1:
        lines.append("    %x = llvm.add %a, %v : i64")
        bad_line = len(lines)
    if i + 1 < n:
        lines.append(f"    %r = llvm.call @f{i + 1}(%s7) : (i64) -> i64")
        lines.append("    llvm.return %r : i64")
    else:
        lines.append("    %p = llvm.mlir.addressof @counter : !llvm.ptr")
        lines.append("    %q = llvm.mlir.addressof @\".str.0\" : !llvm.ptr")
        lines.append("    %r = llvm.mlir.constant(42 : i64) : i64")
        lines.append("    llvm.return %r : i64")
    lines.append("  }")
lines.append("  llvm.func @main() -> i32 {")
lines.append("    %z = llvm.mlir.constant(0 : i64) : i64")
lines.append("    %r = llvm.call @deep(%z) : (i64) -> i64")
lines.append("    %t = llvm.trunc %r : i64 to i32")
lines.append("    llvm.return %t : i32")
lines.append("  }")
# The chain of 3000 calls is too deep for nothing: main calls the last one.
lines.append("  llvm.func @deep(%a: i64) -> i64 {")
lines.append(f"    %r = llvm.call @f{n - 1}(%a) : (i64) -> i64")
lines.append("    llvm.return %r : i64")
lines.append("  }")
lines.append("}")
for j in range(loc):
    lines.append(f'#loc{j} = loc("big.go":{j + 1}:1)')
open(out, "w").write("\n".join(lines) + "\n")
if bad_line:
    print(bad_line, file=sys.stderr)

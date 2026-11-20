# Writes a module for split-adapt.mlir: main returns the low byte of the
# sum of f0(0) .. f(N-1)(N-1), each fK a few instructions, about 3000
# instructions in all. Prints the expected exit code to stderr.
import sys

n = 500
out = ["module {"]
for k in range(n):
    out.append(f'  llvm.func @"main.f{k}"(%x: i64) -> i64 attributes {{no_inline}} {{')
    out.append(f"    %c = llvm.mlir.constant({k} : i64) : i64")
    out.append("    %t = llvm.mlir.constant(3 : i64) : i64")
    out.append("    %a = llvm.add %x, %c : i64")
    out.append("    %b = llvm.mul %a, %t : i64")
    out.append("    %r = llvm.xor %b, %c : i64")
    out.append("    llvm.return %r : i64")
    out.append("  }")
out.append("  llvm.func @main() -> i32 {")
out.append("    %s0 = llvm.mlir.constant(0 : i64) : i64")
total = 0
for k in range(n):
    out.append(f"    %k{k} = llvm.mlir.constant({k} : i64) : i64")
    out.append(f'    %v{k} = llvm.call @"main.f{k}"(%k{k}) : (i64) -> i64')
    out.append(f"    %s{k + 1} = llvm.add %s{k}, %v{k} : i64")
    total += ((k + k) * 3) ^ k
out.append(f"    %m = llvm.mlir.constant(255 : i64) : i64")
out.append(f"    %w = llvm.and %s{n}, %m : i64")
out.append("    %r = llvm.trunc %w : i64 to i32")
out.append("    llvm.return %r : i32")
out.append("  }")
out.append("}")
open(sys.argv[1], "w").write("\n".join(out) + "\n")
print(total & 255, file=sys.stderr)

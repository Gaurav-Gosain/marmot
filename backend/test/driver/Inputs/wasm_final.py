# Writes a small final wasm module to argv[1], laid out as binaryen may write
# one: custom sections first and between the known ones (LLVM's reader
# rejects that order). Imports: env.f (function index 0) and a memory.
# Code bodies (offsets in the code section contents): main.adder$1 at 1,
# main.main at 5, an unnamed function at 8; the code ends at 12.
import sys

def leb(n):
    out = bytearray()
    while True:
        b = n & 0x7F
        n >>= 7
        if n:
            out.append(b | 0x80)
        else:
            out.append(b)
            return bytes(out)

def name(s):
    return leb(len(s)) + s.encode()

def section(id, body):
    return bytes([id]) + leb(len(body)) + body

def custom(n, body):
    return section(0, name(n) + body)

types = leb(1) + bytes([0x60, 0, 0])
imports = leb(2) + name("env") + name("f") + bytes([0]) + leb(0) \
    + name("env") + name("memory") + bytes([2, 1]) + leb(1) + leb(2)
funcs = leb(3) + leb(0) + leb(0) + leb(0)
bodies = [bytes([0, 0x01, 0x0B]), bytes([0, 0x0B]), bytes([0, 0x01, 0x0B])]
code = leb(len(bodies)) + b"".join(leb(len(b)) + b for b in bodies)
fnames = [(0, "env.f"), (1, "main.adder$1"), (2, "main.main")]
sub = leb(len(fnames)) + b"".join(leb(i) + name(n) for i, n in fnames)
names = custom("name", bytes([0]) + leb(len(name("prog"))) + name("prog") + bytes([1]) + leb(len(sub)) + sub)
data = leb(0)
module = b"\0asm" + bytes([1, 0, 0, 0]) + custom("producers", leb(0)) \
    + section(1, types) + section(2, imports) + section(3, funcs) \
    + section(10, code) + names + section(11, data) + custom("target_features", leb(0))
open(sys.argv[1], "wb").write(module)

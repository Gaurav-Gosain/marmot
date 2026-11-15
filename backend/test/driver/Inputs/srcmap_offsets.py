# Prints the code section offsets of the segments of a source map of a wasm
# module: the generated columns (file offsets) minus the start of the code
# section contents. Arguments: MAP MODULE.
import json, sys

B = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

def uleb(b, i):
    v = s = 0
    while True:
        c = b[i]; i += 1
        v |= (c & 0x7F) << s; s += 7
        if c < 0x80:
            return v, i

def code_start(b):
    i = 8
    while i < len(b):
        sid = b[i]
        n, j = uleb(b, i + 1)
        if sid == 10:
            return j
        i = j + n
    raise SystemExit("no code section")

start = code_start(open(sys.argv[2], "rb").read())
col, out = 0, []
for seg in json.load(open(sys.argv[1]))["mappings"].replace(";", ",").split(","):
    if not seg:
        continue
    u = sh = 0
    for ch in seg:
        d = B.index(ch)
        u |= (d & 31) << sh
        sh += 5
        if not d & 32:
            col += -(u >> 1) if u & 1 else u >> 1
            break
    out.append(str(col - start))
print(" ".join(out))

# Reference decoder for the marmot pc table (docs/pcln.md): prints, for each
# pc offset given, "function file:line". Used by the tests and as the model
# for the runtime's lookup.
import struct, sys

def uvarint(b, i):
    v = s = 0
    while True:
        c = b[i]; i += 1
        v |= (c & 0x7F) << s; s += 7
        if c < 0x80:
            return v, i

def lookup(t, off):
    assert t[:8] == b"MRMTPCLN"
    ver, nf, nfiles, funcOff, fileOff, lineOff, strOff = struct.unpack_from("<7I", t, 8)
    s = lambda o: t[strOff + o:t.index(b"\0", strOff + o)].decode()
    for k in range(nf):
        start, size, name, lo, entry = struct.unpack_from("<QIIII", t, funcOff + 24 * k)
        if start <= off < start + size:
            i = lineOff + lo
            n, i = uvarint(t, i)
            f, i = uvarint(t, i)
            pc, line, best = start, entry, (entry, f)
            for _ in range(n):
                d, i = uvarint(t, i)
                z, i = uvarint(t, i)
                v = (z >> 1) ^ -(z & 1)      # zigzag: v = 2*lineDelta + fileChanged
                flag = v & 1
                pc += d
                line += (v - flag) // 2
                if flag:
                    f, i = uvarint(t, i)
                if pc > off:
                    break
                best = (line, f)
            fname = s(struct.unpack_from("<I", t, fileOff + 4 * best[1])[0])
            return f"{s(name)} {fname}:{best[0]}"
    return "?"

t = open(sys.argv[1], "rb").read()
for a in sys.argv[2:]:
    print(lookup(t, int(a, 0)))

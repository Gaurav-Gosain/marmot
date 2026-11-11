# Reference decoder for the marmot pc table, version 2 (docs/pcln.md): prints
# "function file:line" for each pc offset given. The model for the runtime's
# lookup.
import bisect, struct, sys

def uvarint(b, i):
    v = s = 0
    while True:
        c = b[i]; i += 1
        v |= (c & 0x7F) << s; s += 7
        if c < 0x80:
            return v, i

class Table:
    def __init__(self, t):
        assert t[:8] == b"MRMTPCLN"
        (self.version, self.nf, self.nfiles, self.ndirs, self.npkgs, self.quantum,
         self.textEnd, self.funcOff, self.fileOff, self.dirOff, self.pkgOff,
         self.lineOff, self.strOff) = struct.unpack_from("<13I", t, 8)
        assert self.version == 2
        self.t = t
        self.starts = [struct.unpack_from("<I", t, self.funcOff + 12 * k)[0] for k in range(self.nf)]

    def cstr(self, off):
        a = self.strOff + off
        return self.t[a:self.t.index(b"\0", a)]

    def prefixed(self, off, table):
        rec = self.cstr(off)
        idx, n = uvarint(rec, 0)
        head = self.cstr(struct.unpack_from("<I", self.t, table + 4 * (idx - 1))[0])
        return (head + rec[n:]).decode()

    def lookup(self, off):
        k = bisect.bisect_right(self.starts, off) - 1
        if k < 0 or off >= self.textEnd:
            return "?"
        start, name, lo = struct.unpack_from("<III", self.t, self.funcOff + 12 * k)
        i = self.lineOff + lo
        n, i = uvarint(self.t, i)
        line, i = uvarint(self.t, i)
        f, i = uvarint(self.t, i)
        pc = start
        best = (line, f)
        for _ in range(n):
            d, i = uvarint(self.t, i)
            z, i = uvarint(self.t, i)
            v = (z >> 1) ^ -(z & 1)          # zigzag: v = 2*lineDelta + fileChanged
            flag = v & 1
            pc += d * self.quantum
            line += (v - flag) // 2
            if flag:
                f, i = uvarint(self.t, i)
            if pc > off:
                break
            best = (line, f)
        file = self.prefixed(struct.unpack_from("<I", self.t, self.fileOff + 4 * best[1])[0], self.dirOff)
        return f"{self.prefixed(name, self.pkgOff)} {file}:{best[0]}"

t = Table(open(sys.argv[1], "rb").read())
for a in sys.argv[2:]:
    print(t.lookup(int(a, 0)))

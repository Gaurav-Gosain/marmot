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
        assert self.version in (2, 3)
        self.t = t
        self.inl = {}
        if self.version == 3:
            ninl, idx, self.inlOff = struct.unpack_from("<3I", t, 8 + 4 * 13)
            for k in range(ninl):
                fi, off = struct.unpack_from("<II", t, idx + 8 * k)
                self.inl[fi] = off
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
        if best[0] == 0:                     # name only (no DWARF lines)
            return self.prefixed(name, self.pkgOff)
        file = self.prefixed(struct.unpack_from("<I", self.t, self.fileOff + 4 * best[1])[0], self.dirOff)
        return f"{self.prefixed(name, self.pkgOff)} {file}:{best[0]}"

    def name(self, rec):
        return self.prefixed(rec, self.pkgOff)

    def file(self, f):
        return self.prefixed(struct.unpack_from("<I", self.t, self.fileOff + 4 * f)[0], self.dirOff)

    def inline(self, k):
        """Inline tree of function k: nodes (parent, name, file, line) and ranges (lo, hi, node)."""
        i = self.inlOff + self.inl[k]
        n, i = uvarint(self.t, i)
        nodes = []
        for _ in range(n):
            p, i = uvarint(self.t, i)
            nm, i = uvarint(self.t, i)
            f, i = uvarint(self.t, i)
            ln, i = uvarint(self.t, i)
            nodes.append((p - 1, self.name(nm), self.file(f), ln))
        r, i = uvarint(self.t, i)
        ranges, at = [], self.starts[k]
        for _ in range(r):
            gap, i = uvarint(self.t, i)
            ln, i = uvarint(self.t, i)
            node, i = uvarint(self.t, i)
            lo = at + gap * self.quantum
            at = lo + ln * self.quantum
            ranges.append((lo, at, node - 1))
        return nodes, ranges

    def frames(self, off):
        """All frames at pc offset off, innermost first."""
        k = bisect.bisect_right(self.starts, off) - 1
        inner = self.lookup(off)
        if k < 0 or k not in self.inl:
            return [inner]
        nodes, ranges = self.inline(k)
        node = next((n for lo, hi, n in ranges if lo <= off < hi), -1)
        if node < 0:
            return [inner]
        out = [nodes[node][1] + " " + inner.split(" ", 1)[1]]
        while node >= 0:
            parent, _, f, ln = nodes[node]
            pname = nodes[parent][1] if parent >= 0 else inner.split(" ", 1)[0]
            out.append(f"{pname} {f}:{ln}")
            node = parent
        return out

t = Table(open(sys.argv[1], "rb").read())
if sys.argv[2] == "--inline":
    # Frames at the start of every inlined range of the function at the given offset.
    k = bisect.bisect_right(t.starts, int(sys.argv[3], 0)) - 1
    for lo, hi, n in t.inline(k)[1]:
        print(" <- ".join(t.frames(lo)))
else:
    for a in sys.argv[2:]:
        print(t.lookup(int(a, 0)))

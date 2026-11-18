# patch_build_id.py BIN: flip one byte of the ELF build id of BIN in place.
import struct, sys
p = sys.argv[1]
d = bytearray(open(p, 'rb').read())
shoff, = struct.unpack_from('<Q', d, 0x28)
shentsize, shnum, shstrndx = struct.unpack_from('<HHH', d, 0x3a)
hs = [struct.unpack_from('<IIQQQQIIQQ', d, shoff + i * shentsize) for i in range(shnum)]
strtab = hs[shstrndx][4]
for h in hs:
    name = d[strtab + h[0]:d.index(b'\0', strtab + h[0])]
    if name == b'.note.gnu.build-id':
        off = h[4]
        namesz, descsz, typ = struct.unpack_from('<III', d, off)
        desc = off + 12 + ((namesz + 3) & ~3)
        d[desc] ^= 0xff
        open(p, 'wb').write(d)
        sys.exit(0)
sys.exit('no .note.gnu.build-id in ' + p)

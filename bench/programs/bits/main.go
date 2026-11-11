// bit manipulation: popcount by hand, bit reverse and a table-free CRC-32.
package main

const n = 30000000

func popcount(x uint64) int {
	x = x - ((x >> 1) & 0x5555555555555555)
	x = (x & 0x3333333333333333) + ((x >> 2) & 0x3333333333333333)
	x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0f
	return int((x * 0x0101010101010101) >> 56)
}

func reverse(x uint32) uint32 {
	x = (x&0x55555555)<<1 | (x>>1)&0x55555555
	x = (x&0x33333333)<<2 | (x>>2)&0x33333333
	x = (x&0x0f0f0f0f)<<4 | (x>>4)&0x0f0f0f0f
	x = (x&0x00ff00ff)<<8 | (x>>8)&0x00ff00ff
	return x<<16 | x>>16
}

func crc32(crc uint32, b byte) uint32 {
	crc ^= uint32(b)
	for i := 0; i < 8; i++ {
		if crc&1 != 0 {
			crc = crc>>1 ^ 0xedb88320
		} else {
			crc >>= 1
		}
	}
	return crc
}

func main() {
	x := uint64(88172645463325252)
	pc := 0
	var rv uint32
	crc := ^uint32(0)
	for i := 0; i < n; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		pc += popcount(x)
		rv ^= reverse(uint32(x))
		crc = crc32(crc, byte(x))
		crc = crc32(crc, byte(x>>8))
	}
	println("bits", pc, rv, ^crc)
}

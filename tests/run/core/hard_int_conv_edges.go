package main

func main() {
	var f32 float32 = 1e20
	var f64 float64 = -1.5
	println(int64(f64), uint8(int(f64)), int32(f32/1e15))
	var u64 uint64 = 1 << 63
	println(float64(u64), float32(u64), int64(u64))
	var i64 int64 = -1 << 63
	println(float64(i64), uint64(i64), -i64 == i64)
	var u32 uint32 = 1<<32 - 1
	println(float32(u32), float64(u32+1), u32+1)
	x := int8(-128)
	println(x/-1, x%-1, -x, x*x, x-1, x+x)
	var big uint64 = 0xffffffffffffffff
	println(big/3, big%1000, big>>1, int64(big>>1)+1)
	h := uint16(0xffff)
	println(h*h, h+h, h<<4, h>>4, ^h, -h)
	var a, b uint = 5, 7
	println(a-b, int(a)-int(b), a-b > 0)
	var d int = -7
	println(d/2, d%2, d>>1, uint(d)>>60, d&3, d|8, d^-1)
	var sh uint = 65
	println(d<<sh, d>>sh, uint8(200)<<sh)
}

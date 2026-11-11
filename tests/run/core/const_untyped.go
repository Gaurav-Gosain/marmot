package main

const (
	Big   = 1 << 100
	Small = Big >> 99
	F     = 1.0 / 3
	Mix   = 1 + 2.5
	R     = 'a' + 1.0
	S     = "con" + "cat"
	L     = len(S)
	B     = 5 > 3
)

func main() {
	println(Small, F, Mix, R, S, L, B)
	var f32 float32 = F
	var f64 float64 = F
	println(f32, f64)
	var i int = 1e3
	println(i)
	var u8 uint8 = 'a'
	println(u8)
	var c complex128 = 1
	println(real(c))
	var x float64 = 1 << 3
	println(x)
	const typed int8 = 100
	println(typed + 27)
	const e = 2.718281828459045235360287471352662497757247093699959574966967627724076630353
	println(e)
	println(7 / 2 * 2.0)
	println(7 / 2.0 * 2)
	var sh uint = 3
	var res = 1.0 * 4
	println(res, 1<<sh)
	println(len("héllo"), len([3]int{}))
	const m = -5 % 3
	println(m, -5/3)
}

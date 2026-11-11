package main

type Weekday int

const (
	Sunday Weekday = iota
	Monday
	Tuesday
	_
	Thursday
)

const (
	KB = 1 << (10 * (iota + 1))
	MB
	GB
)

const (
	a, b = iota, iota * 10
	c, d
	e, f
)

const (
	x = iota
	y = "str"
	z
	w = iota
)

type Flags uint8

const (
	F1 Flags = 1 << iota
	F2
	F3
	Mask = F1 | F3
)

func main() {
	println(Sunday, Monday, Tuesday, Thursday)
	println(KB, MB, GB)
	println(a, b, c, d, e, f)
	println(x, y, z, w)
	println(F1, F2, F3, Mask)
	const local = iota
	println(local)
}

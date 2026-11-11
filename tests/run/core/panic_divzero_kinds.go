package main

func try(f func()) {
	defer func() { println(recover().(error).Error()) }()
	f()
}

func main() {
	var i8, i16, i32, i64 int8 = 1, 0, 0, 0
	_ = i16
	_ = i32
	_ = i64
	var a8, z8 int8 = 5, 0
	var a32, z32 uint32 = 5, 0
	var a64, z64 int64 = 5, 0
	var az, zz uint = 5, 0
	try(func() { _ = a8 / z8 })
	try(func() { _ = a8 % z8 })
	try(func() { _ = a32 / z32 })
	try(func() { _ = a64 % z64 })
	try(func() { _ = az / zz })
	var f, zf float64 = 1, 0
	println(f/zf, i8)
}

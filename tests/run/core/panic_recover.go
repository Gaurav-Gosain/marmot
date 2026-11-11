package main

func safe(f func()) (msg string) {
	defer func() {
		r := recover()
		if r == nil {
			msg = "no panic"
			return
		}
		switch v := r.(type) {
		case string:
			msg = "string: " + v
		case error:
			msg = "error: " + v.Error()
		case int:
			msg = "int"
		default:
			msg = "other"
		}
	}()
	f()
	return "unreachable?"
}

type MyErr struct{}

func (MyErr) Error() string { return "myerr" }

func main() {
	println(safe(func() {}))
	println(safe(func() { panic("boom") }))
	println(safe(func() { panic(42) }))
	println(safe(func() { panic(MyErr{}) }))
	println(safe(func() { panic(1.5) }))
	println(safe(func() { var a []int; _ = a[5] }))
	println(safe(func() { var p *int; _ = *p }))
	z := 0
	println(safe(func() { _ = 1 / z }))
	println(recover() == nil)
}

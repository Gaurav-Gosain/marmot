package main

type E struct{}

func (*E) Error() string { return "E" }

func mayFail(fail bool) error {
	var e *E
	if fail {
		e = &E{}
	}
	return e
}

func realNil() error { return nil }

func main() {
	err := mayFail(false)
	println(err == nil, err != nil)
	println(realNil() == nil)
	var p *E
	var i interface{} = p
	println(i == nil, i == (*E)(nil), p == nil)
	var j interface{}
	println(j == nil)
	j = nil
	println(j == nil)
	var e error
	println(e == nil)
	e = (*E)(nil)
	println(e == nil)
	var f interface{ Error() string }
	println(f == nil)
}

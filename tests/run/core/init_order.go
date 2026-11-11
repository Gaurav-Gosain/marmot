package main

var (
	a = c + b
	b = f()
	c = g()
	d = 3
)

func f() int { println("f"); return d + 1 }
func g() int { println("g"); return b + 1 }

var x, y = pair()

func pair() (int, int) { println("pair"); return 1, 2 }

var z = func() int { println("z"); return 9 }()

func init() { println("init1", a, b, c, d) }
func init() { println("init2", x, y, z) }

func main() {
	println("main", a)
}

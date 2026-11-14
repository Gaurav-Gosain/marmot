// Boxing values of basic types into interfaces (codegen scalarBox): one
// byte values point into a static table and 2, 4 and 8 byte values go
// through convT16/32/64, which share static words for 0-255. Every value
// must still compare, switch and print as itself, negative and float
// values included.
package main

import (
	"fmt"
	"math"
)

type level int8
type flag bool
type code uint16

//go:noinline
func box(vs ...any) []any { return vs }

func main() {
	var i8 int8 = -1
	var u8 uint8 = 255
	var b = true
	var i16 int16 = -300
	var u16 uint16 = 7
	var i32 int32 = math.MinInt32
	var u32 uint32 = 255
	var i64 int64 = -5
	var u64 uint64 = 256
	var f32 float32 = 1.5
	var f64 = math.Copysign(0, -1)
	var nan = math.NaN()
	var r rune = 'z'
	var by byte = 'A'
	var up uintptr = 12
	var lv level = -3
	var fl flag = true
	var cd code = 65535
	for k := 0; k < 2; k++ {
		vs := box(i8, u8, b, i16, u16, i32, u32, i64, u64, f32, f64, nan, r, by, up, lv, fl, cd, int(k), k == 0)
		fmt.Println(vs...)
		for _, v := range vs {
			switch x := v.(type) {
			case int8:
				fmt.Print("int8 ", x, x == -1, "; ")
			case float64:
				fmt.Print("float64 ", x, math.Signbit(x), x != x, "; ")
			case level:
				fmt.Print("level ", x, x < 0, "; ")
			case code:
				fmt.Print("code ", x, x == 65535, "; ")
			case bool:
				fmt.Print("bool ", x, "; ")
			}
		}
		fmt.Println()
		fmt.Println(vs[0] == any(int8(-1)), vs[1] == any(uint8(255)), vs[2] == any(true), vs[3] == any(int16(-300)),
			vs[4] == any(uint16(7)), vs[8] == any(uint64(256)), vs[10] == any(math.Copysign(0, -1)), vs[11] == vs[11], vs[18] == any(k))
		i8--
		u8++
		b = !b
		u16 += 300
		i64 *= -1
	}
}

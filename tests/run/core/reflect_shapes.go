// reflect.Value.Call through call adapters shared by ABI shape (codegen
// reflectcall.go): signatures of one shape (int and a defined int, pointers
// of different types, structs of the same layout) share an adapter and
// must still pass and return their values. Method tables keep method
// values and method expressions for exported methods only; unexported
// methods still satisfy interfaces.
package main

import (
	"fmt"
	"reflect"
	"sort"
)

type myInt int

type pt struct{ X, Y int }
type size struct{ W, H int }

type T struct{ n int }

func (t T) Get() int           { return t.n }
func (t *T) Set(v int)         { t.n = v }
func (t T) hidden() int        { return -t.n }
func (t T) Pair(a, b int) pt   { return pt{a + t.n, b + t.n} }
func (t T) Mixed(s string) any { return s + fmt.Sprint(t.n) }

type hider interface{ hidden() int }

func call(f any, args ...any) []any {
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		in[i] = reflect.ValueOf(a)
	}
	var out []any
	for _, r := range reflect.ValueOf(f).Call(in) {
		out = append(out, r.Interface())
	}
	return out
}

func main() {
	x, y := 1, 2
	fmt.Println(call(func(a int) int { return a + 1 }, 41))
	fmt.Println(call(func(a myInt) myInt { return a * 2 }, myInt(21)))
	fmt.Println(call(func(p *int) int { return *p }, &x))
	fmt.Println(call(func(p *string) string { return *p + "!" }, new(string)))
	fmt.Println(call(func(p pt) size { return size{p.Y, p.X} }, pt{3, 4}))
	fmt.Println(call(func(s size) pt { return pt{s.H, s.W} }, size{5, 6}))
	fmt.Println(call(func(a, b int) (int, int) { return b, a }, x, y))
	fmt.Println(call(func(b bool, u uint8) (uint8, bool) { return u + 1, !b }, true, uint8(9)))
	fmt.Println(call(func(f float64, i int64) float64 { return f * float64(i) }, 1.5, int64(4)))
	fmt.Println(call(fmt.Sprintf, "%d-%s", 7, "x"))

	t := &T{n: 10}
	v := reflect.ValueOf(t)
	var names []string
	for i := 0; i < v.NumMethod(); i++ {
		names = append(names, v.Type().Method(i).Name)
	}
	sort.Strings(names)
	fmt.Println(v.NumMethod(), names)
	v.MethodByName("Set").Call([]reflect.Value{reflect.ValueOf(5)})
	fmt.Println(v.MethodByName("Get").Call(nil)[0], t.n)
	fmt.Println(v.MethodByName("Pair").Call([]reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2)})[0])
	fmt.Println(v.MethodByName("Mixed").Call([]reflect.Value{reflect.ValueOf("n=")})[0])
	m, ok := reflect.TypeOf(T{}).MethodByName("Pair")
	fmt.Println(ok, m.Type, m.Func.Call([]reflect.Value{reflect.ValueOf(T{1}), reflect.ValueOf(2), reflect.ValueOf(3)})[0])
	fmt.Println(reflect.ValueOf(T{3}).MethodByName("hidden").IsValid())

	var h any = T{n: 4}
	if hh, ok := h.(hider); ok {
		fmt.Println("hidden", hh.hidden())
	}
	f := t.Get
	fmt.Println(reflect.ValueOf(f).Call(nil)[0])
}

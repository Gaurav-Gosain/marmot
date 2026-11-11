package main

import (
	"fmt"
	"reflect"
)

type N struct {
	V    int
	Next *N
}

func main() {
	a := map[string][]int{"k": {1, 2}}
	b := map[string][]int{"k": {1, 2}}
	fmt.Println(reflect.DeepEqual(a, b), reflect.DeepEqual(a, map[string][]int{"k": {1}}))
	fmt.Println(reflect.DeepEqual(&N{1, &N{2, nil}}, &N{1, &N{2, nil}}), reflect.DeepEqual(&N{1, nil}, &N{2, nil}))
	fmt.Println(reflect.DeepEqual([]int(nil), []int{}), reflect.DeepEqual(nil, nil), reflect.DeepEqual(1, int64(1)), reflect.DeepEqual("a", "a"))
	var x, y any = []any{1, "a", nil}, []any{1, "a", nil}
	fmt.Println(reflect.DeepEqual(x, y))
	nan := []float64{0}
	nan[0] = nan[0] / nan[0]
	fmt.Println(reflect.DeepEqual(nan, nan), reflect.DeepEqual(nan, []float64{nan[0]}))
	f := func() {}
	fmt.Println(reflect.DeepEqual(f, f), reflect.DeepEqual((func())(nil), (func())(nil)))
	cyc := &N{1, nil}
	cyc.Next = cyc
	cyc2 := &N{1, nil}
	cyc2.Next = cyc2
	fmt.Println(reflect.DeepEqual(cyc, cyc2))
}

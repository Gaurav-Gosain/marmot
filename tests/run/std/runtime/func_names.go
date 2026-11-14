package main

// runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name() gives gc's names:
// tools match them (bubbletea test fixtures skip commands by name).

import (
	"fmt"
	"reflect"
	"runtime"
)

func plain() int { return 1 }

func adder(n int) func() int { return func() int { return n } }

type T struct{ n int }

func (t *T) Get() int { return t.n }

func (t T) Val() int { return t.n }

func name(f any) string {
	if fn := runtime.FuncForPC(reflect.ValueOf(f).Pointer()); fn != nil {
		return fn.Name()
	}
	return "unknown"
}

func main() {
	t := &T{3}
	fmt.Println(name(plain))
	fmt.Println(name(adder(1)))
	fmt.Println(name(t.Get))
	fmt.Println(name(T{}.Val))
	fmt.Println(name((*T).Get))
	fmt.Println(name(func() {}))
	fmt.Println(name(func() int { return t.n }))
	fmt.Println(name(fmt.Println))
}

package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestAdd(t *testing.T) {
	if add(2, 3) != 5 {
		t.Fatal("add(2, 3) != 5")
	}
}

func TestInitOnce(t *testing.T) {
	if inits != 1 {
		t.Fatalf("init ran %d times", inits)
	}
}

// TestNames logs the names gc gives the package under test at run time.
func TestNames(t *testing.T) {
	rt := reflect.TypeOf(T{})
	t.Logf("%T %s %s %v", T{}, rt.String(), rt.PkgPath(), T{4})
	var s fmt.Stringer = T{5}
	t.Log(s.String())
}

func ExampleT() {
	fmt.Println(T{6})
	// Output: T(6)
}

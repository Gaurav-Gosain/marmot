// Audit (spec, 2026-11-12): panic(nil) panics with *runtime.PanicNilError,
// whose Error() in gc is "runtime error: panic called with nil argument".
// marmot's runtime returns the text without the "runtime error: " prefix.
package main

import (
	"errors"
	"fmt"
	"runtime"
)

func main() {
	defer func() {
		r := recover()
		err, _ := r.(error)
		var re runtime.Error
		fmt.Println(err.Error(), errors.As(err, &re))
		fmt.Printf("%v|%T\n", r, r)
	}()
	panic(nil)
}

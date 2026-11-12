// Package mod has one finding of each kind the check tests look for.
package mod

import (
	"fmt"
	"os"
)

// Hello calls Printf with a wrong verb (vet printf).
func Hello(n int) {
	fmt.Printf("%s\n", n)
}

// Write ignores the error of a write (errcheck) and uses wide
// permissions (gosec G306).
func Write(p string) {
	os.WriteFile(p, nil, 0o644)
}

// Quiet has the same problems, suppressed by a directive.
func Quiet(p string) {
	os.WriteFile(p, nil, 0o644) //nolint:errcheck,gosec // test
}

func unusedHelper() {}

// Count has a loop the modernizer rewrites to range over an int.
func Count(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += i
	}
	return s
}

package diag

import (
	"reflect"
	"testing"
)

func TestWords(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"use `strconv.Itoa(n)` here", []string{"use", "`strconv.Itoa(n)`", "here"}},
		{"has type func() int, needs func(a, b int) (int, error) now", []string{"has", "type", "func() int,", "needs", "func(a, b int) (int, error)", "now"}},
		{"value of type struct { X int } here", []string{"value", "of", "type", "struct", "{ X int }", "here"}},
		{"func() returns nothing (value of struct type Square)", []string{"func()", "returns", "nothing", "(value", "of", "struct", "type", "Square)"}},
		{"type map[string]int and *T", []string{"type", "map[string]int", "and", "*T"}},
	} {
		if got := words(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("words(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

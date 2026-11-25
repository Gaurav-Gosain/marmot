package main

import (
	"slices"
	"testing"
)

// TestWithTestTimeout checks that marmot test gives the test binary go test's
// 10 minute -timeout unless the command line sets one in any form.
func TestWithTestTimeout(t *testing.T) {
	for _, c := range []struct {
		in, want []string
	}{
		{nil, []string{"-test.timeout=10m0s"}},
		{[]string{"-test.v", "-test.run=TestX"}, []string{"-test.timeout=10m0s", "-test.v", "-test.run=TestX"}},
		{[]string{"-test.timeout=30s"}, []string{"-test.timeout=30s"}},
		{[]string{"-test.v", "-test.timeout=0"}, []string{"-test.v", "-test.timeout=0"}},
		{[]string{"-test.timeout"}, []string{"-test.timeout"}},
		{[]string{"-test.timeoutx=1"}, []string{"-test.timeout=10m0s", "-test.timeoutx=1"}},
	} {
		if got := withTestTimeout(slices.Clone(c.in)); !slices.Equal(got, c.want) {
			t.Errorf("withTestTimeout(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

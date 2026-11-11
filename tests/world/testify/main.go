package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rec struct{ msgs []string }

func (r *rec) Errorf(format string, args ...interface{}) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}
func (r *rec) FailNow() { r.msgs = append(r.msgs, "FAILNOW") }

type P struct {
	A int
	B []string
}

func main() {
	r := &rec{}
	var _ testing.TB
	assert.Equal(r, 1, 1)
	assert.Equal(r, 1, 2)
	assert.Equal(r, P{1, []string{"a"}}, P{1, []string{"b"}})
	assert.NotEqual(r, "a", "a")
	assert.True(r, false, "msg %d", 5)
	assert.Nil(r, 5)
	assert.NotNil(r, nil)
	assert.Contains(r, "hello", "ell")
	assert.Contains(r, []int{1, 2}, 3)
	assert.Len(r, []int{1}, 2)
	assert.Empty(r, "x")
	assert.ElementsMatch(r, []int{1, 2, 3}, []int{3, 2, 1})
	assert.ElementsMatch(r, []int{1, 2}, []int{1, 3})
	assert.InDelta(r, 1.0, 1.5, 0.1)
	assert.Error(r, nil)
	assert.EqualError(r, fmt.Errorf("a"), "b")
	assert.JSONEq(r, `{"a":1,"b":2}`, `{"b":2,"a":1}`)
	assert.JSONEq(r, `{"a":1}`, `{"a":2}`)
	assert.Regexp(r, "^a.c$", "abd")
	assert.IsType(r, 1, "s")
	assert.Greater(r, 1, 2)
	for i, m := range r.msgs {
		if k := strings.Index(m, "Error:"); k >= 0 {
			m = m[k:]
		}
		fmt.Printf("--- %d\n%s\n", i, m)
	}
	r2 := &rec{}
	require.Equal(r2, 1, 2)
	fmt.Println(len(r2.msgs))
}

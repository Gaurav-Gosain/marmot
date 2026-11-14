package main_test

import (
	"strings"
	"testing"
)

func TestExternal(t *testing.T) {
	if strings.Repeat("a", 3) != "aaa" {
		t.Fatal("repeat")
	}
}

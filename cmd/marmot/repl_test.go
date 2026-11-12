package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestReplComplete(t *testing.T) {
	for in, want := range map[string]bool{
		"x := 1":               true,
		"for i := 0; i < 2; {": false,
		"f(1,":                 false,
		"x := 1 +":             false,
		"func f() {\n}":        true,
		`s := "{"`:             true,
	} {
		if got := complete(in); got != want {
			t.Errorf("complete(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestReplDefinedNames(t *testing.T) {
	got := definedNames("a, b := 1, 2\nvar c int\nfor i := 0; i < 1; i++ { d := i; _ = d }\n_ = a")
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("definedNames = %v, want %v", got, want)
	}
}

func TestReplMarkWriter(t *testing.T) {
	var out bytes.Buffer
	m := &markWriter{w: &out, buf: &bytes.Buffer{}}
	m.Write([]byte("old output\n" + replMark[:5]))
	m.Write([]byte(replMark[5:] + "new"))
	m.Write([]byte(" output\n"))
	if out.String() != "new output\n" {
		t.Errorf("got %q", out.String())
	}
}

func TestReplProgram(t *testing.T) {
	s := &replSession{imports: map[string]string{"strings": "strings", "os/exec": "exec"}}
	s.stmts = []string{"x := 1"}
	s.names = []string{"x"}
	p := s.program(`__fmt.Println(strings.Repeat("a", x))`, "")
	if strings.Contains(p, `"os/exec"`) {
		t.Errorf("unused import kept:\n%s", p)
	}
	for _, want := range []string{`strings "strings"`, "x := 1\n", "_ = x\n", `__fmt.Println(strings.Repeat("a", x))`} {
		if !strings.Contains(p, want) {
			t.Errorf("program lacks %q:\n%s", want, p)
		}
	}
	if strings.Index(p, "x := 1") > strings.Index(p, "__os.Stdout.WriteString") {
		t.Errorf("session statements must come before the marker:\n%s", p)
	}
}

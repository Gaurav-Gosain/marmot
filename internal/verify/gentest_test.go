package verify

import (
	"go/format"
	"strings"
	"testing"
)

func TestTestFile(t *testing.T) {
	src := TestFile("codec", []GenTest{
		{Name: TestName("example.com/codec.Header", 1), Comment: "codec.go:5:36: index out of range when len(b) == 2", Call: "Header([]byte(\"aa\"))"},
		{Name: TestName("(*example.com/codec.Reader).Read", 1), Comment: "codec.go:9:2: slice bounds out of range", Call: "new(Reader).Read(nil)"},
	})
	if _, err := format.Source([]byte(src)); err != nil {
		t.Fatalf("not valid Go: %v\n%s", err, src)
	}
	for _, want := range []string{"func TestMarmotVerify_Header_1(t *testing.T)", "func TestMarmotVerify_Reader_Read_1(t *testing.T)", "Header([]byte(\"aa\"))", "DO NOT EDIT"} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
}

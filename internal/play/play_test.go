package play

import (
	"strings"
	"testing"
)

func TestFilesAndImports(t *testing.T) {
	s := New(Options{})
	if _, err := s.files(&Request{Txtar: "-- ../x.go --\npackage main\n"}); err == nil {
		t.Error("a path outside the workspace was accepted")
	}
	f, err := s.files(&Request{Txtar: "-- go.mod --\nmodule evil\n-- main.go --\npackage main\n-- b.go --\npackage main\n"})
	if err != nil || len(f) != 2 || f["go.mod"] != nil {
		t.Errorf("txtar: %v %v", f, err)
	}
	d := checkImports(map[string][]byte{"main.go": []byte("package main\n\nimport (\n\t\"C\"\n\t\"fmt\"\n\t\"github.com/a/b\"\n)\n")})
	if len(d) != 2 || d[0].Line != 4 || !strings.Contains(d[1].Message, "github.com/a/b") {
		t.Errorf("imports: %+v", d)
	}
}

func TestMLIRLines(t *testing.T) {
	text := "module {\n  %0 = go.index %a[%b] loc(#loc1)\n  %1 = llvm.add %x, %y loc(#loc3)\n  llvm.return loc(\"/w/main.go\":9:2)\n}\n#loc0 = loc(\"/w/main.go\":7:3)\n#loc1 = loc(\"/w/main.go\":8:5)\n#loc2 = loc(\"/lib/fmt/print.go\":1:1)\n#loc3 = loc(callsite(#loc2 at #loc0))\n"
	out, lines := mlirLines(text, "/w/main.go")
	if strings.Contains(out, "loc(") || strings.Contains(out, "#loc") {
		t.Errorf("locations left:\n%s", out)
	}
	want := []int{0, 8, 7, 9, 0}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d: %d, want %d (%v)", i, lines[i], w, lines)
		}
	}
}

func TestLLVMLines(t *testing.T) {
	text := `define i32 @main.Sum(ptr %0) !dbg !5 {
  %2 = add i32 %1, 1, !dbg !10
  %3 = call i32 @f(), !dbg !12
  ret i32 %3, !dbg !11
}
!1 = !DIFile(filename: "main.go", directory: "/w")
!2 = !DIFile(filename: "print.go", directory: "/lib/fmt")
!5 = distinct !DISubprogram(name: "Sum", scope: !1, file: !1, line: 6)
!6 = distinct !DISubprogram(name: "f", scope: !2, file: !2, line: 3)
!10 = !DILocation(line: 9, column: 5, scope: !5)
!11 = !DILocation(line: 11, column: 2, scope: !5)
!12 = !DILocation(line: 4, column: 1, scope: !6, inlinedAt: !10)
`
	out, lines := llvmLines(text, "/w/main.go")
	if strings.Contains(out, "!dbg") || strings.Contains(out, "DILocation") {
		t.Errorf("metadata left:\n%s", out)
	}
	want := []int{6, 9, 9, 11, 0}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d: %d, want %d (%v)", i, lines[i], w, lines)
		}
	}
}

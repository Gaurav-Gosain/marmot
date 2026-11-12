package driver

import (
	"strings"
	"testing"
)

func TestAliasTypes(t *testing.T) {
	s := "!llvm.struct<(ptr, i64)>"
	n := "!llvm.struct<(i64, !llvm.struct<(ptr, ptr)>)>"
	in := "module {\n" +
		strings.Repeat("  %a = llvm.call @f() : () -> "+s+"\n", 4) +
		strings.Repeat("  %b = llvm.call @g() : () -> "+n+"\n", 4) +
		"  %c = llvm.call @h() : () -> !llvm.struct<(f64)>\n" +
		"  llvm.mlir.global internal @s(\"!llvm.struct<(ptr, i64)>\") : !llvm.array<24 x i8>\n" +
		"}\n"
	out := aliasTypes(in)
	if !strings.HasPrefix(out, "!mt0 = "+n+"\n!mt1 = "+s+"\n") {
		t.Fatalf("aliases not defined first:\n%s", out)
	}
	if got := strings.Count(out, "-> !mt1\n"); got != 4 {
		t.Errorf("uses of !mt1: %d, want 4", got)
	}
	if got := strings.Count(out, "-> !mt0\n"); got != 4 {
		t.Errorf("uses of !mt0 (nested struct as one type): %d, want 4", got)
	}
	if !strings.Contains(out, "!llvm.struct<(f64)>") {
		t.Errorf("a type used once was aliased")
	}
	if !strings.Contains(out, `@s("!llvm.struct<(ptr, i64)>")`) {
		t.Errorf("text inside a string literal changed")
	}
	if aliasTypes("module {\n}\n") != "module {\n}\n" {
		t.Errorf("text without repeated types changed")
	}
}

package driver

import (
	"path/filepath"
	"testing"
)

// TestLLVMBinEnv: LLVM_BIN comes first, and the tools are found in it.
func TestLLVMBinEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LLVM_BIN", dir)
	if d, from := LLVMBin(); d != dir || from != "LLVM_BIN" {
		t.Errorf("LLVMBin() = %q, %q; want %q, LLVM_BIN", d, from, dir)
	}
	if got := LLVMTool("clang"); got != filepath.Join(dir, "clang") {
		t.Errorf("LLVMTool(clang) = %q", got)
	}
}

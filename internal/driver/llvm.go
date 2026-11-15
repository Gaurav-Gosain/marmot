package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// homebrewLLVM is the bin directory of Homebrew's llvm@22 on Apple silicon.
const homebrewLLVM = "/opt/homebrew/opt/llvm@22/bin"

// LLVMRecord is the file in the marmot root where scripts/install.sh writes
// the bin directory of the LLVM marmot was built with.
const LLVMRecord = "build/llvm-bin"

// recordedLLVM is the bin directory in the root's LLVMRecord, "" when there
// is none or it has no clang. Read once.
var recordedLLVM = sync.OnceValues(func() (dir, file string) {
	root, err := FindRoot()
	if err != nil {
		return "", ""
	}
	file = filepath.Join(root, LLVMRecord)
	b, err := os.ReadFile(file)
	if err != nil {
		return "", ""
	}
	dir = strings.TrimSpace(string(b))
	if dir == "" || !fileExists(filepath.Join(dir, "clang")) {
		return "", ""
	}
	return dir, file
})

// LLVMBin is the directory marmot runs LLVM's tools from and where that
// came from: $LLVM_BIN, the install record, or Homebrew's llvm@22. Without
// any of them dir is "" and the tools are looked up on PATH (name-22, then
// name).
func LLVMBin() (dir, source string) {
	if d := os.Getenv("LLVM_BIN"); d != "" {
		return d, "LLVM_BIN"
	}
	if d, f := recordedLLVM(); d != "" {
		return d, "the install record " + f
	}
	if fileExists(filepath.Join(homebrewLLVM, "clang")) {
		return homebrewLLVM, "Homebrew llvm@22"
	}
	return "", "PATH"
}

// LLVMTool is the path of an LLVM tool (clang, llvm-ar, ld.lld, ...): in
// LLVMBin's directory, else on PATH; else the Homebrew path, so that the
// error names a place to install it.
func LLVMTool(name string) string {
	if d, _ := LLVMBin(); d != "" {
		return filepath.Join(d, name)
	}
	for _, n := range []string{name + "-22", name} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return filepath.Join(homebrewLLVM, name)
}

func llvmTool(name string) string { return LLVMTool(name) }

package bind

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func clang(t *testing.T) string {
	for _, c := range []string{os.Getenv("LLVM_BIN") + "/clang", "/opt/homebrew/opt/llvm@22/bin/clang", "clang"} {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	t.Skip("no clang")
	return ""
}

// TestDemo binds testdata/demo/demo.h with its bind.toml and compares the
// Go file and the report with the goldens; a second run gives the same
// bytes.
func TestDemo(t *testing.T) {
	dir := filepath.Join("testdata", "demo")
	cfg, err := LoadConfig(filepath.Join(dir, "bind.toml"))
	if err != nil {
		t.Fatal(err)
	}
	header, _ := filepath.Abs(filepath.Join(dir, "demo.h"))
	run := func() (string, string) {
		h, err := Parse(clang(t), header, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		src, report, err := Generate(cfg, h, "demo.h")
		if err != nil {
			t.Fatalf("%v\n%s", err, src)
		}
		return string(src), strings.Join(report, "\n") + "\n"
	}
	src, report := run()
	src2, report2 := run()
	if src != src2 || report != report2 {
		t.Errorf("two runs differ")
	}
	golden(t, filepath.Join(dir, "demo.go.golden"), src)
	golden(t, filepath.Join(dir, "report.golden"), report)
	for _, want := range []string{
		"func Open(path string) (*DB, error)",             // out handle + status
		"func (h *DB) Close() error",                      // [close]
		"func (h *DB) Put(key string, data []byte) error", // string, (ptr, len)
		"func (h *DB) Count() int",
		"func (h *DB) Errmsg() string",
		"func (h *DB) Name() string", // owned: copied and freed
		"func (h *DB) DBColor() Color",
		"func Move(p *Point, dx int, dy int)",
		"func Fopen(name string) *File", // pointer typedef handle
		"func (h *File) Close() error",
		"func (h *DB) Size() (int, error)", // [out] scalar
		"(FlagA << 1) // DEMO_FLAG_B",
		"func (v Color) String() string",
		"Red   Color = 0",
		"Blue  Color = 6",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	// The generated file is valid cgo: go vet type checks it through cgo.
	if goCmd, err := exec.LookPath("go"); err == nil {
		mod := t.TempDir()
		hdr, _ := os.ReadFile(header)
		files := map[string]string{"go.mod": "module example.com/demo\n\ngo 1.24\n", "demo.go": src, "demo.h": string(hdr)}
		for n, c := range files {
			if err := os.WriteFile(filepath.Join(mod, n), []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// The binding includes <demo.h>: find it in the module directory.
		if err := os.WriteFile(filepath.Join(mod, "flags.go"), []byte("package demo\n\n// #cgo CFLAGS: -I${SRCDIR}\nimport \"C\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(goCmd, "vet", ".")
		cmd.Dir = mod
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("go vet of the generated binding: %v\n%s", err, out)
		}
	}
	for _, want := range []string{"demo_log: not bound: variadic", "demo_unowned: not bound: returns char *: who frees it?"} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func golden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs (go test -update to accept)\n%s", path, got)
	}
}

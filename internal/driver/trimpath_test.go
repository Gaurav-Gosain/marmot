package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestTrimName(t *testing.T) {
	src := filepath.FromSlash("/m/goroot/src/")
	main := &packages.Package{PkgPath: "example.com/app/cmd/app", Module: &packages.Module{Path: "example.com/app", Dir: filepath.FromSlash("/home/u/app")}}
	dep := &packages.Package{PkgPath: "golang.org/x/text/width", Module: &packages.Module{Path: "golang.org/x/text", Version: "v0.20.0", Dir: filepath.FromSlash("/home/u/go/pkg/mod/golang.org/x/text@v0.20.0")}}
	std := &packages.Package{PkgPath: "runtime"}
	for _, c := range []struct {
		p    *packages.Package
		file string
		want string
	}{
		{std, "/m/goroot/src/runtime/proc.go", "runtime/proc.go"},
		{main, "/home/u/app/cmd/app/main.go", "example.com/app/cmd/app/main.go"},
		{dep, "/home/u/go/pkg/mod/golang.org/x/text@v0.20.0/width/kind.go", "golang.org/x/text@v0.20.0/width/kind.go"},
		// a file outside its module (generated in a cache): package path and name
		{main, "/home/u/.cache/marmot/cgo/abc/_cgo_gotypes.go", "example.com/app/cmd/app/_cgo_gotypes.go"},
	} {
		if got := trimName(src, c.p, filepath.FromSlash(c.file)); got != c.want {
			t.Errorf("trimName(%s) = %s, want %s", c.file, got, c.want)
		}
	}
}

// A -trimpath build of a program that panics holds no directory of this
// machine: not the checkout, the home directory, the program's directory,
// the merged or the real GOROOT, in any mode, for the host, wasip1/wasm and
// a Linux cross build (when ld.lld is there).
func TestTrimPathBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds nine programs")
	}
	root := os.Getenv("MARMOT_ROOT") // a checkout with an installed build (a worktree has none)
	if root == "" {
		r, err := filepath.Abs("../..")
		if err != nil {
			t.Fatal(err)
		}
		root = r
	}
	if _, err := os.Stat(filepath.Join(root, "build", "bin", "marmot-backend")); err != nil {
		t.Skip("no installed backend")
	}
	dir := t.TempDir()
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	src := filepath.Join(dir, "prog")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"go.mod":  "module example.com/trim\n\ngo 1.24\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\txs := []int{1, 2}\n\tfor i := 0; i <= len(xs); i++ {\n\t\tfmt.Println(xs[i])\n\t}\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	private := []string{root, src, dir}
	if h, err := os.UserHomeDir(); err == nil && h != "" && h != "/" {
		private = append(private, h)
	}
	if out, err := exec.Command("go", "env", "GOROOT").Output(); err == nil {
		if g := strings.TrimSpace(string(out)); g != "" {
			private = append(private, g)
			if r, err := filepath.EvalSymlinks(g); err == nil {
				private = append(private, r)
			}
		}
	}
	targets := []string{"", "wasip1/wasm"}
	if ldLLD() != "" {
		targets = append(targets, "linux/amd64")
	}
	for _, target := range targets {
		for _, mode := range []string{"dev", "O2", "release"} {
			name := strings.ReplaceAll(target, "/", "-") + "-" + mode
			if target == "" {
				name = runtime.GOOS + "-" + runtime.GOARCH + "-" + mode
			}
			t.Run(name, func(t *testing.T) {
				out := filepath.Join(dir, name)
				cfg := Config{Root: root, Dir: src, Output: out, OptLevel: "2", Emit: "exe", GC: "precise", Dialect: "llvm",
					Target: target, Patterns: []string{"."}, Stderr: os.Stderr, TrimPath: true}
				switch mode {
				case "O2":
					cfg.OptSet = true
				case "release":
					cfg.OptLevel, cfg.Dialect, cfg.Release = "3", "go", true
				}
				if _, err := Build(cfg); err != nil {
					t.Fatal(err)
				}
				// a cross build without the target's sysroot writes a directory to link on
				// the target (objects, the runtime library, link.sh): check all of it
				var data []byte
				err := filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return err
					}
					b, err := os.ReadFile(p)
					data = append(data, b...)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range private {
					if i := bytes.Index(data, []byte(p)); i >= 0 {
						end := min(len(data), i+len(p)+40)
						t.Errorf("output holds %s: %q", p, data[i:end])
					}
				}
				if !bytes.Contains(data, []byte("example.com/trim")) {
					t.Errorf("no example.com/trim in the output: the pc table names the program's file by its module path")
				}
				if target != "" {
					return
				}
				// the traceback names the file as -trimpath recorded it
				cmd := exec.Command(out)
				cmd.Env = append(os.Environ(), "MARMOT_OUTPUT=plain")
				msg, _ := cmd.CombinedOutput()
				if !bytes.Contains(msg, []byte("\texample.com/trim/main.go:")) {
					t.Errorf("traceback does not name example.com/trim/main.go:\n%s", msg)
				}
			})
		}
	}
}

package driver

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestMajorOf(t *testing.T) {
	for path, want := range map[string]string{
		"example.com/app":    "v1",
		"example.com/app/v2": "v2",
		"example.com/v1":     "v1",
		"gopkg.in/yaml.v3":   "v1",
	} {
		if got := majorOf(path); got != want {
			t.Errorf("majorOf(%q) = %q, want %q", path, got, want)
		}
	}
}

// The object's .go.buildinfo section reads back with debug/buildinfo, as
// go version -m reads it, and the text parses as runtime/debug parses it.
func TestBuildInfoObject(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("ELF and Mach-O only")
	}
	if _, err := exec.LookPath(llvmTool("clang")); err != nil {
		t.Skip("no clang")
	}
	bi := &debug.BuildInfo{GoVersion: "go1.27", Path: "example.com/app",
		Main:     debug.Module{Path: "example.com/app", Version: "v0.0.0-20261127000000-abcdefabcdef"},
		Deps:     []*debug.Module{{Path: "golang.org/x/text", Version: "v0.20.0", Sum: "h1:abc="}},
		Settings: []debug.BuildSetting{{Key: "-compiler", Value: "gc"}, {Key: "vcs.time", Value: time.Unix(0, 0).UTC().Format(time.RFC3339)}}}
	text := modinfoStart + bi.String() + modinfoEnd
	b := &build{work: t.TempDir(), goos: runtime.GOOS, goarch: runtime.GOARCH}
	obj, err := b.buildInfoObject("go1.27", text)
	if err != nil {
		t.Fatal(err)
	}
	// debug/buildinfo reads segments, so the object goes into an executable.
	mainC := filepath.Join(b.work, "main.c")
	if err := os.WriteFile(mainC, []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(b.work, "exe")
	if out, err := exec.Command(llvmTool("clang"), mainC, obj, "-o", exe).CombinedOutput(); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	got, err := buildinfo.ReadFile(exe)
	if err != nil {
		t.Fatalf("debug/buildinfo: %v", err)
	}
	if got.GoVersion != "go1.27" || got.Path != "example.com/app" || got.Main.Version != bi.Main.Version || len(got.Deps) != 1 || got.Deps[0].Sum != "h1:abc=" {
		t.Errorf("read back %+v", got)
	}
	if !strings.Contains(got.String(), "build\t-compiler=gc") {
		t.Errorf("settings lost:\n%s", got)
	}
}

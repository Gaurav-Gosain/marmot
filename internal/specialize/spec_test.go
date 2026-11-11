package specialize

import (
	"bytes"
	"context"
	"encoding/json"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

// The differential tests build each program twice with the stock Go
// toolchain: as written, and as rewritten by this package (through go build
// -overlay, with marmot_spec.go added to GOROOT/src/fmt). The outputs must
// be byte identical, and the rewrite must have fired.

func goroot(t *testing.T) string {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// specOverlay loads dir/main.go, rewrites it and returns an overlay file for
// go build plus the number of rewritten sites.
func specOverlay(t *testing.T, dir string) (string, int, string) {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedTypesSizes | packages.NeedModule,
		Dir: dir,
		Env: append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off"),
	}
	libFiles := libOverlay(t)
	cfg.Overlay = map[string][]byte{}
	for dst, src := range libFiles {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Overlay[dst] = b
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	var all []*packages.Package
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		all = append(all, p)
		for _, e := range p.Errors {
			t.Fatalf("%s: %v", p.PkgPath, e)
		}
	})
	res, err := Rewrite(all)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	repl := libOverlay(t)
	var gen string
	for _, p := range res.Changed {
		for _, f := range p.Syntax {
			name := p.Fset.File(f.Pos()).Name()
			var src string
			if filepath.Base(name) == GenFile {
				src = res.Gen[p]
				gen = src
			} else {
				var buf bytes.Buffer
				if err := format.Node(&buf, p.Fset, f); err != nil {
					t.Fatal(err)
				}
				src = buf.String()
			}
			tmp := filepath.Join(work, filepath.Base(name))
			if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			repl[name] = tmp
		}
	}
	js, _ := json.Marshal(map[string]any{"Replace": repl})
	ov := filepath.Join(work, "overlay.json")
	if err := os.WriteFile(ov, js, 0o644); err != nil {
		t.Fatal(err)
	}
	if d := os.Getenv("SPEC_TEST_KEEP"); d != "" {
		os.WriteFile(filepath.Join(d, "overlay.json"), js, 0o644)
	}
	return ov, res.Sites, gen
}

// libOverlay maps GOROOT/src paths to the entry point files under _lib.
func libOverlay(t *testing.T) map[string]string {
	m := map[string]string{}
	root := goroot(t)
	err := filepath.WalkDir("_lib", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel("_lib", path)
		m[filepath.Join(root, "src", rel)] = mustAbs(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func mustAbs(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// runGo builds dir with go build (and the overlay, if any) and runs it.
func runGo(t *testing.T, dir, overlay string) (string, int) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "prog")
	args := []string{"build", "-o", exe}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append(args, ".")...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off", "PWD="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build (overlay %v): %v\n%s", overlay != "", err, out)
	}
	if overlay != "" {
		// The generated functions must be in the binary, or the overlay
		// was not applied and the test compares the program with itself.
		nm, err := exec.Command("go", "tool", "nm", exe).Output()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(nm, []byte("marmot_spec_")) {
			t.Fatalf("specialized binary has no marmot_spec_ symbols: overlay not applied")
		}
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Minute)
	defer cancel2()
	run := exec.CommandContext(ctx2, exe)
	var out bytes.Buffer
	run.Stdout = &out
	run.Stderr = &out
	err := run.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), code
}

// diffTest writes src as a program and compares stock and specialized runs.
func diffTest(t *testing.T, src string, minSites int) {
	t.Helper()
	// Resolve symlinks (macOS /var is /private/var): the go command
	// matches overlay paths against its own view of the directory.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module spectest\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := os.Getenv("SPEC_TEST_PROGRAMS"); d != "" {
		// Keep a copy of the program, to run it under marmot as well.
		pd := filepath.Join(d, strings.ToLower(strings.TrimPrefix(t.Name(), "Test")))
		os.MkdirAll(pd, 0o755)
		os.WriteFile(filepath.Join(pd, "main.go"), []byte(src), 0o644)
	}
	ov, sites, gen := specOverlay(t, dir)
	if sites < minSites {
		t.Fatalf("rewrote %d sites, want at least %d\n%s", sites, minSites, gen)
	}
	want, wcode := runGo(t, dir, "")
	got, gcode := runGo(t, dir, ov)
	if want != got || wcode != gcode {
		wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
		for i := 0; i < len(wl) && i < len(gl); i++ {
			if wl[i] != gl[i] {
				t.Fatalf("line %d differs\nstock:       %q\nspecialized: %q\n(exit %d vs %d)", i+1, wl[i], gl[i], wcode, gcode)
			}
		}
		t.Fatalf("outputs differ in length: %d vs %d lines (exit %d vs %d)", len(wl), len(gl), wcode, gcode)
	}
	if testing.Verbose() {
		t.Logf("%d sites, %d output bytes", sites, len(want))
	}
}

func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestParseFormat(t *testing.T) {
	cases := []struct {
		f    string
		n    int
		ok   bool
		want int // number of ops
	}{
		{"%d", 1, true, 1},
		{"a%db", 1, true, 3},
		{"%%", 0, true, 1},
		{"%5.2f|%-8s|%+v|%#v|%q", 5, true, 9},
		{"%d %d", 1, false, 0}, // missing
		{"%d", 2, false, 0},    // extra
		{"%[1]d", 1, false, 0}, // index
		{"%*d", 2, false, 0},   // star
		{"%", 0, false, 0},     // no verb
		{"%5.", 0, true, 1},    // '.' is the verb: %!.(MISSING)? no: missing
		{"%99999999d", 1, false, 0},
	}
	for _, c := range cases {
		ops, ok := parseFormat(c.f, c.n, false)
		if c.f == "%5." {
			// '.' is the verb and there is no operand: MISSING, not rewritten.
			if ok {
				t.Errorf("%q: rewritten", c.f)
			}
			continue
		}
		if ok != c.ok || (ok && len(ops) != c.want) {
			t.Errorf("%q n=%d: ok=%v ops=%d, want ok=%v ops=%d", c.f, c.n, ok, len(ops), c.ok, c.want)
		}
	}
}

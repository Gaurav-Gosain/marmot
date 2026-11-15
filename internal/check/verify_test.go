package check

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

const verifyModule = `package codec

// ReadU16 reads a big-endian uint16 at off.
func ReadU16(b []byte, off int) uint16 {
	return uint16(b[off])<<8 | uint16(b[off+1])
}

func Header(b []byte) (kind byte, n uint16) {
	if len(b) < 2 {
		return 0, 0
	}
	return b[0], ReadU16(b, 1)
}
`

// TestVerifyLatentFiles checks that LatentFiles reports latent findings
// in those files only.
func TestVerifyLatentFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("loads packages with go list")
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/codec\n\ngo 1.22\n",
		"a.go":   "package codec\n\nfunc At(s []int, i int) int { return s[i] }\n",
		"b.go":   "package codec\n\nfunc B(s []int) int { return s[0] }\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := filepath.EvalSymlinks(filepath.Join(dir, "a.go"))
	res, err := Run(Options{Dir: dir, Only: []string{"verify"}, Offline: true, LatentFiles: map[string]bool{a: true, filepath.Join(dir, "a.go"): true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Checks {
		if c.Name != "verify" {
			continue
		}
		if len(c.Findings) != 1 || filepath.Base(c.Findings[0].Span.File) != "a.go" {
			t.Errorf("findings %v, want one in a.go", c.Findings)
		}
	}
}

func TestParseStatusZ(t *testing.T) {
	out := " M a.go\x00R  new.go\x00old.go\x00?? x/c.go\x00 D gone.txt\x00C  copy.go\x00orig.go\x00"
	got := parseStatusZ("/r", []byte(out))
	for _, want := range []string{"/r/a.go", "/r/new.go", "/r/x/c.go", "/r/copy.go"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	for _, not := range []string{"/r/old.go", "/r/orig.go", "/r/ld.go", "/r/rig.go"} {
		if got[not] {
			t.Errorf("has %s", not)
		}
	}
}

// TestVerifyPolicy writes a policy, then removes a guard: the function
// that now leaves an index open must be a policy error.
func TestVerifyPolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("loads packages with go list")
	}
	dir := t.TempDir()
	good := "package codec\n\nfunc At(s []int, i int) int {\n\tif i < 0 || i >= len(s) {\n\t\treturn 0\n\t}\n\treturn s[i]\n}\n"
	bad := "package codec\n\nfunc At(s []int, i int) int {\n\treturn s[i]\n}\n"
	run := func(src string) []fnStat {
		for name, text := range map[string]string{"go.mod": "module example.com/codec\n\ngo 1.22\n", "codec.go": src} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		res, err := Run(Options{Dir: dir, Only: []string{"verify"}, Offline: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Checks {
			if c.Name == "verify" {
				return c.fnstats
			}
		}
		return nil
	}
	if err := writePolicy(dir, run(good), nil, nil); err != nil {
		t.Fatal(err)
	}
	p, err := readPolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ds := checkPolicy(p, run(good)); len(ds) != 0 {
		t.Errorf("unchanged code: %v", ds)
	}
	ds := checkPolicy(p, run(bad))
	if len(ds) == 0 || !strings.Contains(ds[0].Message, "example.com/codec.At") || ds[0].Severity != diag.Error {
		t.Fatalf("regression not reported: %v", ds)
	}
	if sp := ds[0].Span; filepath.Base(sp.File) != "codec.go" || sp.Line != 3 {
		t.Errorf("policy error at %s:%d, want codec.go:3 (the function)", sp.File, sp.Line)
	}
}

// TestVerifyPolicyMain runs marmot verify with the package cache and the
// run memo on: an unchanged rerun of a failing tree must fail again, and
// -write-policy on one package must keep the baseline of the others.
func TestVerifyPolicyMain(t *testing.T) {
	if testing.Short() {
		t.Skip("loads packages with go list")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MARMOT_OUTPUT", "plain") // JSON runs skip the memo; plain runs use it
	t.Chdir(dir)
	good := "package codec\n\nfunc At(s []int, i int) int {\n\tif i < 0 || i >= len(s) {\n\t\treturn 0\n\t}\n\treturn s[i]\n}\n"
	bad := "package codec\n\nfunc At(s []int, i int) int {\n\treturn s[i]\n}\n"
	sub := "package sub\n\nfunc Get(s []int) int {\n\tif len(s) == 0 {\n\t\treturn 0\n\t}\n\treturn s[0]\n}\n"
	put := func(name, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("go.mod", "module example.com/codec\n\ngo 1.22\n")
	put("codec.go", good)
	put("sub/sub.go", sub)
	main := func(args ...string) int {
		t.Helper()
		stdout, stderr := os.Stdout, os.Stderr
		null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout, os.Stderr = null, null
		defer func() { os.Stdout, os.Stderr = stdout, stderr; null.Close() }()
		return VerifyMain(args, "test")
	}
	if code := main("-write-policy", "./..."); code != 0 {
		t.Fatalf("-write-policy: exit %d", code)
	}
	p, err := readPolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Baseline["example.com/codec.At"]; !ok {
		t.Fatalf("baseline has no codec.At: %v", p.Baseline)
	}
	if code := main("./..."); code != 0 {
		t.Errorf("unchanged tree: exit %d", code)
	}
	put("codec.go", bad)
	for run := 1; run <= 2; run++ {
		if code := main("./..."); code != 1 {
			t.Errorf("regression, run %d: exit %d, want 1", run, code)
		}
	}
	put("codec.go", good)
	main("./...") // memoize the good tree
	if code := main("-write-policy", "./sub"); code != 0 {
		t.Fatalf("-write-policy ./sub: exit %d", code)
	}
	p, err = readPolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"example.com/codec.At", "example.com/codec/sub.Get"} {
		if _, ok := p.Baseline[fn]; !ok {
			t.Errorf("after -write-policy ./sub, baseline has no %s: %v", fn, p.Baseline)
		}
	}
}

// TestVerifySearchHintsCached runs the verify check twice on an unchanged
// tree with the package cache: both runs must have the search hints that
// confirmation needs (they ride in the cached findings).
func TestVerifySearchHintsCached(t *testing.T) {
	if testing.Short() {
		t.Skip("loads packages with go list")
	}
	dir := t.TempDir()
	for name, text := range map[string]string{"go.mod": "module example.com/codec\n\ngo 1.22\n", "codec.go": verifyModule} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cache := t.TempDir()
	for run := 1; run <= 2; run++ {
		res, err := Run(Options{Dir: dir, Only: []string{"verify"}, Offline: true, CacheDir: cache, NoMemo: true})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range res.Checks {
			if c.Name == "verify" {
				n = len(c.search)
			}
		}
		if n == 0 {
			t.Errorf("run %d: no search hints", run)
		}
	}
}

// TestVerifyJSON runs the verify check on the design's example and checks
// the schema 1 contract of marmot verify -json: diagnostic lines, then one
// result line with the verify summary.
func TestVerifyJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("loads packages with go list")
	}
	dir := t.TempDir()
	for name, text := range map[string]string{"go.mod": "module example.com/codec\n\ngo 1.22\n", "codec.go": verifyModule} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, latent := range []bool{false, true} {
		res, err := Run(Options{Dir: dir, Only: []string{"verify"}, Latent: latent, Offline: true})
		if err != nil {
			t.Fatal(err)
		}
		var c *CheckResult
		for _, x := range res.Checks {
			if x.Name == "verify" {
				c = x
			}
		}
		if c == nil || c.Verify == nil {
			t.Fatalf("latent=%v: no verify check or summary", latent)
		}
		var buf bytes.Buffer
		writeVerifyJSON(&buf, c.Findings, c.Verify, 0, 0)
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		manifest, latentN := 0, 0
		for i, l := range lines {
			var m map[string]any
			if err := json.Unmarshal([]byte(l), &m); err != nil {
				t.Fatalf("line %d: %v", i+1, err)
			}
			if m["schema"] != float64(diag.Schema) {
				t.Errorf("line %d: schema %v", i+1, m["schema"])
			}
			last := i == len(lines)-1
			kind, _ := m["kind"].(string)
			if last != (kind == "result") || !last && kind != "diagnostic" {
				t.Errorf("line %d: kind %q", i+1, kind)
			}
			if last {
				for _, k := range []string{"ok", "errors", "warnings", "durationMs", "verify"} {
					if _, ok := m[k]; !ok {
						t.Errorf("result: no %s", k)
					}
				}
				continue
			}
			if m["code"] != diag.CodeVerify || m["severity"] != "note" {
				t.Errorf("line %d: code %v severity %v", i+1, m["code"], m["severity"])
			}
			v, _ := m["verify"].(map[string]any)
			switch v["class"] {
			case "manifest":
				manifest++
				if v["condition"] != "len(b) == 2" {
					t.Errorf("condition %v", v["condition"])
				}
			case "latent":
				latentN++
			}
		}
		if manifest != 1 || latent && latentN != 2 || !latent && latentN != 0 {
			t.Errorf("latent=%v: %d manifest, %d latent findings", latent, manifest, latentN)
		}
	}
}

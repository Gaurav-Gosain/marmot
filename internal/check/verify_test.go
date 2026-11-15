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

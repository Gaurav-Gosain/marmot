package driver

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every MARMOT_* switch the code generator reads changes generated code, so
// it must be part of the package keys (writeNoFlags hashes all MARMOT_*
// variables except noCodeEffect). A switch read by codegen must therefore
// never be listed in noCodeEffect; otherwise a cached module built without
// it would be reused with it, and a perfcmp with -b-env would compare
// identical objects.
func TestCodegenSwitchesInKeys(t *testing.T) {
	files, err := filepath.Glob("../codegen/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("codegen sources: %v", err)
	}
	re := regexp.MustCompile(`Getenv\("(MARMOT_[A-Z0-9_]+)"\)|lifetime(?:On|Default)\("(MARMOT_[A-Z0-9_]+)"\)`)
	seen := map[string]bool{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			name := m[1] + m[2]
			seen[name] = true
			if noCodeEffect[name] {
				t.Errorf("%s: codegen reads %s, which noCodeEffect leaves out of the cache keys", filepath.Base(f), name)
			}
		}
	}
	// Switches the driver reads that change the output (link flags, link
	// order, sections): the driver also reads switches that only set paths
	// or limits, so these are listed.
	for _, name := range []string{"MARMOT_NOPTR", "MARMOT_GCVARS", "MARMOT_INITSECT", "MARMOT_NOPIE", "MARMOT_PART_ORDER",
		"MARMOT_HOME_INSTANCES", "MARMOT_LOC_COLS", "MARMOT_WASM_HOSTCALLERS", "MARMOT_LD", "MARMOT_LDFLAGS", "MARMOT_SPLIT", "MARMOT_PART_INSTRS", "MARMOT_BUILDINFO"} {
		if noCodeEffect[name] {
			t.Errorf("the driver output switch %s is in noCodeEffect", name)
		}
		seen[name] = true
	}
	for _, name := range []string{"MARMOT_APPENDSTACK", "MARMOT_NILDOM", "MARMOT_ESCAPE_EXT"} {
		if !seen[name] {
			t.Errorf("the scan did not find %s: the pattern no longer matches how codegen reads switches", name)
		}
	}
	// Every switch is in the key: setting one changes keyBase. MARMOT_DEBUG_*
	// variables only print diagnostics and are left out by design.
	for name := range seen {
		if strings.HasPrefix(name, "MARMOT_DEBUG_") {
			continue
		}
		b := &build{}
		b.initKeyBase()
		before := b.keyBase
		t.Setenv(name, "1")
		b2 := &build{}
		b2.initKeyBase()
		if b2.keyBase == before {
			t.Errorf("%s=1 does not change the package key base", name)
		}
	}
}

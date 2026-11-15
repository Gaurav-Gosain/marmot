package driver

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// A new MARMOT_* switch changes the cache keys without being listed anywhere;
// paths and limits do not.
func TestSwitchesInKeys(t *testing.T) {
	key := func() string {
		var b strings.Builder
		writeNoFlags(&b)
		return b.String()
	}
	t.Setenv("MARMOT_SOME_NEW_SWITCH", "")
	t.Setenv("MARMOT_MAX_JOBS", "2")
	base := key()
	t.Setenv("MARMOT_SOME_NEW_SWITCH", "1")
	if key() == base {
		t.Errorf("a code switch did not change the key")
	}
	t.Setenv("MARMOT_SOME_NEW_SWITCH", "")
	t.Setenv("MARMOT_MAX_JOBS", "4")
	if key() != base {
		t.Errorf("MARMOT_MAX_JOBS changed the key")
	}
}

// A file an overlay adds or replaces is keyed by its contents: two builds
// whose overlays give the same path different contents must not share
// cached modules (the path has the same signature on disk both times).
func TestOverlayInSourceKey(t *testing.T) {
	key := func(src string) string {
		f := "/nonexistent/pkg/x_test.go"
		b := &build{cfg: Config{Overlay: map[string][]byte{f: []byte(src)}}, keyMemo: map[string]string{}, keyBase: "base"}
		return b.srcKeyLocked(&packages.Package{PkgPath: "p", GoFiles: []string{f}})
	}
	if key("package p // A") == key("package p // B") {
		t.Error("overlays with different contents give the same package key")
	}
	if key("package p // A") != key("package p // A") {
		t.Error("the same overlay gives different package keys")
	}
}

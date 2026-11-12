package driver

import (
	"strings"
	"testing"
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

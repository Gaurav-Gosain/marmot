package driver

import (
	"os"
	"testing"
)

// TestInitSections checks the default for each target and the
// MARMOT_INITSECT override.
func TestInitSections(t *testing.T) {
	t.Setenv("MARMOT_INITSECT", "")
	os.Unsetenv("MARMOT_INITSECT")
	for goos, want := range map[string]bool{"linux": true, "darwin": true, "wasip1": true, "ios": false} {
		if got := initSections(goos); got != want {
			t.Errorf("default %s: got %v, want %v", goos, got, want)
		}
	}
	t.Setenv("MARMOT_INITSECT", "0")
	if initSections("linux") || initSections("darwin") {
		t.Error("MARMOT_INITSECT=0: still on")
	}
	t.Setenv("MARMOT_INITSECT", "1")
	if !initSections("ios") {
		t.Error("MARMOT_INITSECT=1: ios still off")
	}
}

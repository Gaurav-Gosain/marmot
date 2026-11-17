package driver

import (
	"path/filepath"
	"strings"
	"testing"
)

// -nocache builds compile in the same mode as cached builds (the incremental
// front end, separate compilation), with a types cache in the work directory,
// so they give the output of a cold cached build. Before, -nocache turned the
// incremental front end off and produced different code (spectralnorm -O 2:
// .text 104249 against 103865 bytes).
func TestNoCacheKeepsIncremental(t *testing.T) {
	t.Setenv("MARMOT_INCREMENTAL", "")
	work := t.TempDir()
	b := &build{cfg: Config{Root: "/r", Emit: "exe", OptLevel: "2", NoCache: true}, work: work}
	if !b.incremental() {
		t.Fatal("-nocache turned the incremental front end off")
	}
	if got := b.typesCache(); !strings.HasPrefix(got, work) {
		t.Errorf("-nocache types cache %s, want a directory in the work directory %s", got, work)
	}
	b.cfg.NoCache = false
	if got, want := b.typesCache(), filepath.Join("/r", "build", "cache", "types"); got != want {
		t.Errorf("types cache %s, want %s", got, want)
	}
}

package driver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A filehash memo that a build reads is marked used. Otherwise the cache
// trim, which deletes the least recently used files first, deletes the memo
// files first (written once, they look the oldest), and every build hashes
// the 90 MB backend again.
func TestToolIDMemoTouched(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "marmot-backend")
	if err := os.WriteFile(tool, []byte("backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	memo := filepath.Join(dir, "cache", "filehash")
	id := contentID(memo, tool)
	es, err := os.ReadDir(memo)
	if err != nil || len(es) != 1 {
		t.Fatalf("memo: %v entries, error %v; want 1", len(es), err)
	}
	mf := filepath.Join(memo, es[0].Name())
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(mf, old, old); err != nil {
		t.Fatal(err)
	}
	contentIDs.Clear() // a new process: only the memo file knows the hash
	if got := contentID(memo, tool); got != id {
		t.Fatalf("contentID from the memo = %s, want %s", got, id)
	}
	fi, err := os.Stat(mf)
	if err != nil {
		t.Fatal(err)
	}
	if age := time.Since(fi.ModTime()); age > time.Hour {
		t.Errorf("the memo read from the file is %v old: a hit does not mark it used", age.Round(time.Second))
	}
}

package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Tool identity in the package keys. A cached module and the objects the
// backend caches under its key depend on more than the marmot binary:
// marmot-backend compiles them, marmot-pcln links their pc table
// fragments, and rtcore.bc is linked into them. keyBase hashes the content
// of these files, so a rebuilt tool misses the cache, and the key does not
// depend on where the tree is.

// writeToolIDs writes the content identity of the tools into a key.
func (b *build) writeToolIDs(w io.Writer) {
	bin := filepath.Join(b.cfg.Root, "build", "bin")
	memo := ""
	if b.cfg.Root != "" && !b.cfg.NoCache {
		memo = filepath.Join(cacheDir(b.cfg.Root), "filehash")
	}
	for _, p := range []string{filepath.Join(bin, "marmot-backend"), filepath.Join(bin, "marmot-pcln"), b.rtLib("rtcore.bc")} {
		fmt.Fprintf(w, "tool %s %s\n", filepath.Base(p), contentID(memo, p))
	}
}

// contentIDs memoizes contentID in this process, by path, size and
// modification time.
var contentIDs sync.Map

// contentID returns the SHA-256 of the file at path, or "missing". The
// backend binary is about 90 MB, so a hash is kept in this process and, when
// memo is not "", in a file under memo, valid while the file keeps its size
// and modification time (as the go command's file hash cache).
func contentID(memo, path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	sig := strconv.FormatInt(fi.Size(), 10) + " " + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
	abs, _ := filepath.Abs(path)
	if v, ok := contentIDs.Load(abs); ok {
		if s, id, _ := strings.Cut(v.(string), "="); s == sig {
			return id
		}
	}
	mf := ""
	if memo != "" {
		h := sha256.Sum256([]byte(abs))
		mf = filepath.Join(memo, hex.EncodeToString(h[:12]))
		if data, err := os.ReadFile(mf); err == nil {
			if s, id, ok := strings.Cut(strings.TrimSpace(string(data)), "="); ok && s == sig && len(id) == 64 {
				// A hit marks the memo used, as a hit on any cache entry
				// does: the cache trim deletes the least recently used
				// files first, and a memo written once would always look
				// the oldest and go first.
				touch(mf)
				contentIDs.Store(abs, sig+"="+id)
				return id
			}
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return "missing"
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	_ = f.Close() // read only
	if err != nil {
		return "unreadable"
	}
	id := hex.EncodeToString(h.Sum(nil))
	contentIDs.Store(abs, sig+"="+id)
	if mf != "" && os.MkdirAll(memo, 0o755) == nil {
		// Temp file and rename: a reader never sees half a line.
		if tf, err := os.CreateTemp(memo, ".tmp*"); err == nil {
			_, werr := tf.WriteString(sig + "=" + id + "\n")
			if cerr := tf.Close(); werr == nil && cerr == nil {
				_ = os.Rename(tf.Name(), mf) // a lost race only costs a hash next time
			}
			_ = os.Remove(tf.Name()) // gone after a rename; the temp file otherwise
		}
	}
	return id
}

package check

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"path/filepath"
	"time"
)

// store is a content addressed cache: one file per key, written atomically.
// It lives under the marmot build cache (build/cache/check), so the cache
// size cap and its eviction cover it. A nil store caches nothing.
type store struct {
	dir string
}

func openStore(dir string) *store {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	return &store{dir: dir}
}

func (s *store) path(key string) string {
	return filepath.Join(s.dir, key[:2], key)
}

// get returns the entry for key, or nil.
func (s *store) get(key string) []byte {
	if s == nil {
		return nil
	}
	p := s.path(key)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	now := time.Now()
	_ = os.Chtimes(p, now, now) // marks the entry used for the trim; a miss of the mark only ages it
	return data
}

// put writes the entry for key. Errors are ignored: a failed write only
// costs a later miss.
func (s *store) put(key string, data []byte) {
	if s == nil {
		return
	}
	p := s.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-")
	if err != nil {
		return
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil || os.Rename(f.Name(), p) != nil {
		_ = os.Remove(f.Name()) // best effort: the temp file of a failed write
	}
}

// hasher builds cache keys from labeled parts.
type hasher struct {
	h hash.Hash
}

func newHasher(kind string) *hasher {
	h := sha256.New()
	hs := &hasher{h: h}
	hs.str(kind)
	return hs
}

func (h *hasher) str(parts ...string) {
	for _, p := range parts {
		h.h.Write([]byte(p))
		h.h.Write([]byte{0})
	}
}

func (h *hasher) bytes(b []byte) {
	sum := sha256.Sum256(b)
	h.h.Write(sum[:])
}

func (h *hasher) sum() string {
	return hex.EncodeToString(h.h.Sum(nil))
}

// fileHash returns the sha256 of a file's contents, or "" when it cannot
// be read.
func fileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }() // read only
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

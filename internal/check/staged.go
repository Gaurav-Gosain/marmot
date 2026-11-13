package check

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// marmot check -staged checks what git has staged, for a pre-commit hook:
// a file with unstaged edits is checked as it is in the index. The index
// is materialized in a directory of the check cache that stays the same
// for the repository, so the per-package caches apply from one commit to
// the next. Files whose working copy equals the index are hard linked
// from the working tree (copied when linking fails); the others are
// written from their blobs. Nothing in the repository changes.

type stagedManifest struct {
	Blobs map[string]string // path in the tree -> blob id written there
	Stats map[string]string // its size and time then: a working file edited in place changes a link
}

// stagedTree materializes the index of the repository around dir under
// cacheDir and returns the directory that corresponds to dir and the
// repository root.
func stagedTree(dir, cacheDir string) (snapDir, root string, err error) {
	root = strings.TrimSpace(gitOut(dir, "rev-parse", "--show-toplevel"))
	if root == "" {
		return "", "", fmt.Errorf("%s is not in a git repository", dir)
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "marmot-check")
	}
	sum := sha256.Sum256([]byte(root))
	base := filepath.Join(cacheDir, "staged", hex.EncodeToString(sum[:8]))
	tree := filepath.Join(base, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		return "", "", err
	}
	// Run resolves symbolic links in its directory (on macOS /var is
	// /private/var); so does the tree, for the findings to map back.
	if t, err := filepath.EvalSymlinks(tree); err == nil {
		tree = t
	}
	var man stagedManifest
	if !readGobFile(filepath.Join(base, "manifest"), &man) || man.Stats == nil {
		man = stagedManifest{Blobs: map[string]string{}, Stats: map[string]string{}}
	}
	record := func(p, dst, blob string) {
		man.Blobs[p] = blob
		if info, err := os.Lstat(dst); err == nil {
			man.Stats[p] = statKey(info)
		}
	}

	// The index: mode, blob id, stage and path.
	ls, err := gitBytes(root, "ls-files", "-s", "-z")
	if err != nil {
		return "", "", err
	}
	type entry struct{ mode, blob string }
	index := map[string]entry{}
	for rec := range bytes.SplitSeq(ls, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		head, path, ok := bytes.Cut(rec, []byte{'\t'})
		f := strings.Fields(string(head))
		if !ok || len(f) != 3 || f[2] != "0" {
			continue // unmerged entries (stage 1 to 3) are not checked
		}
		if f[0] == "160000" {
			continue // a submodule
		}
		index[string(path)] = entry{f[0], f[1]}
	}
	// Paths whose working copy differs from the index.
	dirty := map[string]bool{}
	if out, err := gitBytes(root, "diff", "--name-only", "-z"); err == nil {
		for p := range bytes.SplitSeq(out, []byte{0}) {
			if len(p) > 0 {
				dirty[string(p)] = true
			}
		}
	}
	var need []string // blob ids to write from git
	var needPath []string
	for p, e := range index {
		dst := filepath.Join(tree, filepath.FromSlash(p))
		if man.Blobs[p] == e.blob {
			if info, err := os.Lstat(dst); err == nil && statKey(info) == man.Stats[p] {
				continue
			}
		}
		_ = os.Remove(dst) // replaced below; a missing file is fine
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", "", err
		}
		src := filepath.Join(root, filepath.FromSlash(p))
		if !dirty[p] && e.mode != "120000" {
			if os.Link(src, dst) == nil || copyFile(src, dst) == nil {
				record(p, dst, e.blob)
				continue
			}
		}
		need = append(need, e.blob)
		needPath = append(needPath, p)
	}
	if len(need) > 0 {
		if err := catBlobs(root, need, func(i int, data []byte) error {
			p := needPath[i]
			dst := filepath.Join(tree, filepath.FromSlash(p))
			if index[p].mode == "120000" {
				if err := os.Symlink(string(data), dst); err != nil {
					return err
				}
			} else {
				perm := os.FileMode(0o644)
				if index[p].mode == "100755" {
					perm = 0o755
				}
				if err := os.WriteFile(dst, data, perm); err != nil {
					return err
				}
			}
			record(p, dst, index[p].blob)
			return nil
		}); err != nil {
			return "", "", err
		}
	}
	// Files no longer staged.
	for p := range man.Blobs {
		if _, ok := index[p]; !ok {
			_ = os.Remove(filepath.Join(tree, filepath.FromSlash(p))) // stale copy; the next run retries
			delete(man.Blobs, p)
			delete(man.Stats, p)
		}
	}
	if err := writeGobFile(filepath.Join(base, "manifest"), man); err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("%s is outside %s", dir, root)
	}
	return filepath.Join(tree, rel), root, nil
}

// catBlobs reads blobs with one git cat-file --batch.
func catBlobs(root string, ids []string, f func(i int, data []byte) error) error {
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(strings.Join(ids, "\n") + "\n")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	r := bufio.NewReader(out)
	for i := range ids {
		head, err := r.ReadString('\n')
		if err != nil {
			_ = cmd.Wait() // the read error is the one to report
			return err
		}
		f2 := strings.Fields(head)
		if len(f2) != 3 {
			_ = cmd.Wait() // as above
			return fmt.Errorf("git cat-file: %s", strings.TrimSpace(head))
		}
		n, _ := strconv.Atoi(f2[2])
		data := make([]byte, n+1) // the content and a newline
		if _, err := io.ReadFull(r, data); err != nil {
			_ = cmd.Wait() // as above
			return err
		}
		if err := f(i, data[:n]); err != nil {
			_ = cmd.Wait() // as above
			return err
		}
	}
	return cmd.Wait()
}

func gitBytes(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v", strings.Join(args, " "), err)
	}
	return out, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm())
}

func readGobFile(p string, v any) bool {
	data, err := os.ReadFile(p)
	return err == nil && gob.NewDecoder(bytes.NewReader(data)).Decode(v) == nil
}

// writeGobFile writes through a temporary file and a rename.
func writeGobFile(p string, v any) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp*")
	if err != nil {
		return err
	}
	_, err = f.Write(buf.Bytes())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), p)
	}
	if err != nil {
		_ = os.Remove(f.Name()) // best effort: the temp file of a failed write
	}
	return err
}

// mapPaths rewrites the file names of the findings from the staged tree
// to the repository, where the user reads them.
func mapPaths(res *Result, from, to string) {
	m := func(p string) string {
		if p == from || strings.HasPrefix(p, from+string(filepath.Separator)) {
			return to + p[len(from):]
		}
		return p
	}
	res.Dir = m(res.Dir)
	for _, c := range res.Checks {
		for i := range c.Findings {
			d := &c.Findings[i]
			d.Span.File = m(d.Span.File)
			for j := range d.Related {
				d.Related[j].Span.File = m(d.Related[j].Span.File)
			}
			for j := range d.Fixes {
				for k := range d.Fixes[j].Edits {
					e := &d.Fixes[j].Edits[k]
					e.File, e.Span.File = m(e.File), m(e.Span.File)
				}
			}
		}
		for i, n := range c.Notes {
			c.Notes[i] = strings.ReplaceAll(n, from, to)
		}
	}
}

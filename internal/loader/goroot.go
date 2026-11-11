package loader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MergedGOROOT builds (or reuses) a GOROOT directory under cacheDir in which
// packages found in libSrc replace the stock packages of realRoot. Everything
// else is a symlink into realRoot, so `go list` sees an ordinary GOROOT.
//
// An overlay package directory replaces only the Go files of the stock
// package. Stock subdirectories (other packages) stay visible unless they are
// overlaid too.
func MergedGOROOT(realRoot, libSrc, cacheDir string) (string, error) {
	key, err := overlayKey(realRoot, libSrc)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(cacheDir, "goroot-"+key)
	if _, err := os.Stat(filepath.Join(dst, ".complete")); err == nil {
		return dst, nil
	}
	tmp := dst + fmt.Sprintf(".tmp%d", os.Getpid())
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	ents, err := os.ReadDir(realRoot)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		if e.Name() == "src" {
			continue
		}
		if err := os.Symlink(filepath.Join(realRoot, e.Name()), filepath.Join(tmp, e.Name())); err != nil {
			return "", err
		}
	}
	if err := mergeDir(filepath.Join(tmp, "src"), filepath.Join(realRoot, "src"), libSrc); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".complete"), nil, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.RemoveAll(tmp)
		if _, err2 := os.Stat(filepath.Join(dst, ".complete")); err2 == nil {
			return dst, nil
		}
		return "", err
	}
	return dst, nil
}

// mergeDir creates dst as a real directory. stock may not exist; overlay may
// be "" or not exist.
func mergeDir(dst, stock, overlay string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	stockEnts, _ := os.ReadDir(stock)
	var ovEnts []os.DirEntry
	if overlay != "" {
		ovEnts, _ = os.ReadDir(overlay)
	}
	ovHasGo := false
	ovDirs := map[string]bool{}
	// lib/src/go.mod only keeps lib/src out of the root module.
	kept := ovEnts[:0:0]
	for _, e := range ovEnts {
		if e.Name() != "go.mod" && e.Name() != "go.sum" {
			kept = append(kept, e)
		}
	}
	ovEnts = kept
	// A MERGE file keeps the stock files: overlay files with the same name
	// replace them, and "-name" lines hide stock files.
	merge := false
	hidden := map[string]bool{}
	if data, err := os.ReadFile(filepath.Join(overlay, "MERGE")); overlay != "" && err == nil {
		merge = true
		hidden["MERGE"] = true
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "-") {
				hidden[strings.TrimSpace(line[1:])] = true
			}
		}
		kept = ovEnts[:0:0]
		for _, e := range ovEnts {
			if e.Name() != "MERGE" {
				kept = append(kept, e)
			}
		}
		ovEnts = kept
	}
	for _, e := range ovEnts {
		if e.IsDir() {
			ovDirs[e.Name()] = true
		} else if strings.HasSuffix(e.Name(), ".go") {
			ovHasGo = true
		}
	}
	done := map[string]bool{}
	// Overlay files and directories.
	for _, e := range ovEnts {
		name := e.Name()
		done[name] = true
		if e.IsDir() {
			if err := mergeDir(filepath.Join(dst, name), filepath.Join(stock, name), filepath.Join(overlay, name)); err != nil {
				return err
			}
			continue
		}
		if err := os.Symlink(filepath.Join(overlay, name), filepath.Join(dst, name)); err != nil {
			return err
		}
	}
	for _, e := range stockEnts {
		name := e.Name()
		if done[name] {
			continue
		}
		isDir := e.IsDir()
		if !isDir && e.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(stock, name)); err == nil {
				isDir = fi.IsDir()
			}
		}
		// A package replaced by the overlay loses all of its stock files.
		if ovHasGo && !isDir && !merge {
			continue
		}
		if hidden[name] {
			continue
		}
		if err := os.Symlink(filepath.Join(stock, name), filepath.Join(dst, name)); err != nil {
			return err
		}
	}
	return nil
}

// overlayKey hashes the overlay file list (names, sizes, mtimes are not
// needed: the merged tree links to the files, so only the set of paths
// matters) and the real GOROOT path.
func overlayKey(realRoot, libSrc string) (string, error) {
	var paths []string
	err := filepath.WalkDir(libSrc, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(libSrc, p)
		if d.IsDir() {
			rel += "/"
		}
		if d.Name() == "MERGE" {
			data, _ := os.ReadFile(p)
			rel += "\x00" + string(data)
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n", realRoot, libSrc)
	for _, p := range paths {
		fmt.Fprintln(h, p)
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

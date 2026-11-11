package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/codegen"
	"github.com/Gaurav-Gosain/marmot/internal/loader"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// Up-to-date check for unchanged rebuilds. The stamp hashes everything a
// build reads: the marmot, backend and runtime core binaries, the flags,
// and the size and modification time of every Go source file (like the go
// command's file hashing shortcut). When the stamp matches the stamp of a
// previous build, its cached executable is copied and nothing else runs.

func fileSig(h interface{ Write([]byte) (int, error) }, path string) {
	if fi, err := os.Stat(path); err == nil {
		fmt.Fprintf(h, "%s %d %d\n", path, fi.Size(), fi.ModTime().UnixNano())
	} else {
		fmt.Fprintf(h, "%s missing\n", path)
	}
}

// buildStamp returns the stamp of a build, or "" if it cannot be computed.
func (b *build) buildStamp(goos, goarch string) string {
	files, goroot, err := loader.InputFiles(loader.Config{
		Dir: b.cfg.Dir, Root: b.cfg.Root, Tags: b.cfg.Tags, GOOS: goos, GOARCH: goarch, Patterns: b.cfg.Patterns,
	})
	if err != nil {
		return ""
	}
	sort.Strings(files)
	h := sha256.New()
	fmt.Fprintf(h, "stamp v1\n%s\n%s/%s\n%s %s %s %s %v\n%v\n", goroot, goos, goarch, b.cfg.OptLevel, b.cfg.Emit, b.cfg.Dialect, b.cfg.GC, b.cfg.Release, b.cfg.Tags)
	if exe, err := os.Executable(); err == nil {
		fileSig(h, exe)
	}
	fileSig(h, filepath.Join(b.cfg.Root, "build", "bin", "marmot-backend"))
	fileSig(h, filepath.Join(b.cfg.Root, "build", "lib", "librtcore.a"))
	fileSig(h, filepath.Join(b.cfg.Root, "build", "lib", "rtcore.bc"))
	for _, k := range []string{"MARMOT_LIBSRC", "MARMOT_NO_BACKEND", "MARMOT_DIALECT", "MARMOT_KEEP_SYMBOLS", "MARMOT_LINKMAP", "MARMOT_WPO"} {
		fmt.Fprintf(h, "%s=%s\n", k, os.Getenv(k))
	}
	for _, f := range files {
		fileSig(h, f)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func stampExe(root, stamp string) string {
	return filepath.Join(root, "build", "cache", "exe-"+stamp)
}

// upToDate copies the executable of a previous identical build to out.
func (b *build) upToDate(stamp, out string) bool {
	if stamp == "" || b.cfg.NoCache || b.cfg.Emit != "exe" || b.cfg.Work {
		return false
	}
	cached := stampExe(b.cfg.Root, stamp)
	if !fileExists(cached) {
		return false
	}
	return copyExe(cached, out) == nil
}

// saveStamp records out as the result of the build with this stamp.
func (b *build) saveStamp(stamp, out string) {
	if stamp == "" || b.cfg.NoCache || b.cfg.Emit != "exe" {
		return
	}
	cached := stampExe(b.cfg.Root, stamp)
	tmp := cached + ".tmp"
	if copyExe(out, tmp) == nil {
		os.Rename(tmp, cached)
	}
}

func copyExe(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if strings.HasSuffix(dst, ".tmp") {
		return os.WriteFile(dst, data, 0o755)
	}
	tmp := dst + ".marmot-tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// packageKeys computes, per package, a key over everything its module
// depends on: its source files, its dependencies' keys, the whole-program
// facts codegen uses (codegen.Program.PackageKey), the flags and the
// marmot binary. "" means the package is not cacheable.
func (b *build) packageKeys(lp *loader.Program, cg *codegen.Program, pkgs []*ssa.Package) []string {
	exeSig := sha256.New()
	if exe, err := os.Executable(); err == nil {
		fileSig(exeSig, exe)
	}
	fmt.Fprintf(exeSig, "%s %s %s %s %v\n", b.cfg.OptLevel, b.cfg.Emit, b.cfg.Dialect, b.cfg.GC, b.cfg.Release)
	for _, k := range []string{"MARMOT_LIBSRC", "MARMOT_DIALECT"} {
		fmt.Fprintf(exeSig, "%s=%s\n", k, os.Getenv(k))
	}
	base := hex.EncodeToString(exeSig.Sum(nil))
	byPath := map[string]*packages.Package{}
	for _, p := range lp.All {
		byPath[p.PkgPath] = p
	}
	memo := map[string]string{}
	var key func(p *packages.Package) string
	key = func(p *packages.Package) string {
		if k, ok := memo[p.PkgPath]; ok {
			return k
		}
		memo[p.PkgPath] = "" // cycle guard
		h := sha256.New()
		fmt.Fprintf(h, "pkg v1 %s %s\n", base, p.PkgPath)
		files := append([]string{}, p.CompiledGoFiles...)
		sort.Strings(files)
		for _, f := range files {
			fileSig(h, f)
		}
		deps := make([]string, 0, len(p.Imports))
		for path := range p.Imports {
			deps = append(deps, path)
		}
		sort.Strings(deps)
		for _, d := range deps {
			fmt.Fprintf(h, "dep %s %s\n", d, key(p.Imports[d]))
		}
		k := hex.EncodeToString(h.Sum(nil))[:32]
		memo[p.PkgPath] = k
		return k
	}
	out := make([]string, len(pkgs))
	for i, sp := range pkgs {
		p := byPath[sp.Pkg.Path()]
		if p == nil || sp.Pkg.Path() == "command-line-arguments" {
			continue // file arguments: the overlay import file is not on disk
		}
		h := sha256.New()
		fmt.Fprintf(h, "%s\n%s\n", key(p), cg.PackageKey(sp))
		out[i] = hex.EncodeToString(h.Sum(nil))[:32]
	}
	return out
}

func (b *build) pkgCacheFile(k string) string {
	return filepath.Join(b.cfg.Root, "build", "cache", "pkg-"+k)
}

// cachedPackageObject returns the object key recorded for package key k.
func (b *build) cachedPackageObject(k string) (string, bool) {
	data, err := os.ReadFile(b.pkgCacheFile(k))
	if err != nil {
		return "", false
	}
	obj := strings.TrimSpace(string(data))
	if !fileExists(filepath.Join(b.cfg.Root, "build", "cache", obj+".o")) {
		return "", false
	}
	return obj, true
}

func (b *build) savePackageObject(k, obj string) {
	tmp := b.pkgCacheFile(k) + ".tmp"
	if os.WriteFile(tmp, []byte(obj+"\n"), 0o644) == nil {
		os.Rename(tmp, b.pkgCacheFile(k))
	}
}

// cachedWPO returns the objects of a previous whole-program build.
func (b *build) cachedWPO(k string, n int) ([]string, bool) {
	data, err := os.ReadFile(filepath.Join(b.cfg.Root, "build", "cache", "wpo-"+k))
	if err != nil {
		return nil, false
	}
	keys := strings.Fields(string(data))
	if len(keys) != n {
		return nil, false
	}
	objs := make([]string, n)
	for i, key := range keys {
		objs[i] = filepath.Join(b.cfg.Root, "build", "cache", key+".o")
		if !fileExists(objs[i]) {
			return nil, false
		}
	}
	return objs, true
}

func (b *build) saveWPO(k string, keys []string) {
	p := filepath.Join(b.cfg.Root, "build", "cache", "wpo-"+k)
	if os.WriteFile(p+".tmp", []byte(strings.Join(keys, "\n")+"\n"), 0o644) == nil {
		os.Rename(p+".tmp", p)
	}
}

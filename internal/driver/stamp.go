package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
	lcfg := loader.Config{
		Dir: b.cfg.Dir, Root: b.cfg.Root, Tags: b.cfg.Tags, GOOS: goos, GOARCH: goarch, Patterns: b.cfg.Patterns,
	}
	files, goroot, ok := []string(nil), "", false
	if b.incremental() {
		lcfg.CacheDir = b.typesCache()
		files, goroot, ok = loader.CachedInputFiles(lcfg)
	}
	if !ok {
		var err error
		if files, goroot, err = loader.InputFiles(lcfg); err != nil {
			return ""
		}
	}
	sort.Strings(files)
	h := sha256.New()
	fmt.Fprintf(h, "stamp v1\n%s\n%s/%s\n%s %s %s %s %v tier0=%v\n%v\n", goroot, goos, goarch, b.cfg.OptLevel, b.cfg.Emit, b.cfg.Dialect, b.cfg.GC, b.cfg.Release, b.tier0(), b.cfg.Tags)
	if exe, err := os.Executable(); err == nil {
		fileSig(h, exe)
	}
	fileSig(h, filepath.Join(b.cfg.Root, "build", "bin", "marmot-backend"))
	fileSig(h, filepath.Join(b.cfg.Root, "build", "bin", "marmot-pcln"))
	fileSig(h, b.rtLib("librtcore.a"))
	fileSig(h, b.rtLib("rtcore.bc"))
	for _, k := range []string{"MARMOT_LIBSRC", "MARMOT_NO_BACKEND", "MARMOT_DIALECT", "MARMOT_KEEP_SYMBOLS", "MARMOT_LINKMAP", "MARMOT_WPO", "MARMOT_SPEC", "MARMOT_BACKEND_FLAGS", "MARMOT_PCLN_VERSION"} {
		fmt.Fprintf(h, "%s=%s\n", k, os.Getenv(k))
	}
	writeNoFlags(h)
	b.writeXDefs(h)
	for _, f := range files {
		fileSig(h, f)
	}
	fmt.Fprintf(h, "pgo-gen=%v strip=%v %s trace=%s\n", b.cfg.PGOGen, b.cfg.Strip, os.Getenv("MARMOT_STRIP"), b.cfg.Trace)
	if b.cfg.PGO != "" {
		fileSig(h, b.cfg.PGO)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func stampExe(root, stamp string) string {
	return filepath.Join(root, "build", "cache", "exe-"+stamp)
}

// upToDate copies the executable of a previous identical build to out.
func (b *build) upToDate(stamp, out string) bool {
	if stamp == "" || b.cfg.NoCache || b.cfg.Emit != "exe" || b.cfg.Work || b.cross() {
		return false
	}
	cached := stampExe(b.cfg.Root, stamp)
	if !fileExists(cached) {
		return false
	}
	touch(cached)
	return copyExe(cached, out) == nil
}

// saveStamp records out as the result of the build with this stamp.
func (b *build) saveStamp(stamp, out string) {
	if stamp == "" || b.cfg.NoCache || b.cfg.Emit != "exe" || b.cross() {
		return
	}
	// A temporary name of this process's own: two builds copying to one
	// name could rename a half written executable into the cache.
	cached := stampExe(b.cfg.Root, stamp)
	tmp := fmt.Sprintf("%s.%d.tmp", cached, os.Getpid())
	if err := copyExe(out, tmp); err != nil {
		b.logf("executable cache: %v", err)
		_ = os.Remove(tmp) // best effort: the temp file of a failed copy
	} else if err := os.Rename(tmp, cached); err != nil {
		b.logf("executable cache: %v", err)
		_ = os.Remove(tmp) // as above
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
	byPath := map[string]*packages.Package{}
	for _, p := range lp.All {
		byPath[p.PkgPath] = p
	}
	out := make([]string, len(pkgs))
	for i, sp := range pkgs {
		p := byPath[sp.Pkg.Path()]
		if p == nil || sp.Pkg.Path() == "command-line-arguments" {
			continue // file arguments: the overlay import file is not on disk
		}
		if b.incr {
			if ip := lp.Incremental[p.PkgPath]; apiKeys && ip != nil && ip.ModBase != "" {
				out[i] = b.moduleKey(ip.ModBase)
			} else {
				out[i] = b.moduleKeyFromSource(p)
			}
			continue
		}
		h := sha256.New()
		fmt.Fprintf(h, "%s\n%s\n", b.srcKey(p), cg.PackageKey(sp))
		out[i] = hex.EncodeToString(h.Sum(nil))[:32]
	}
	return out
}

// apiKeys selects the module keys of incremental builds (rule 11 switch,
// default off until gated): with MARMOT_API_KEYS=1 a module is keyed by its
// dependencies' API (moduleKey), else by their source (moduleKeyFromSource).
var apiKeys = os.Getenv("MARMOT_API_KEYS") == "1"

// moduleKey is the key of a package's module in an incremental build,
// where no whole-program facts enter a module: the flags and the marmot
// binary, and the loader's ModBase of the package (its files, its imports'
// API and facts, its generic dependencies' source). Separate compilation
// makes a module depend on nothing else (codegen's sameUnit).
func (b *build) moduleKey(base string) string {
	b.keyMu.Lock()
	b.initKeyBase()
	kb := b.keyBase
	b.keyMu.Unlock()
	h := sha256.New()
	fmt.Fprintf(h, "%s\nseparate v2 %s\n%s\n", kb, b.progFlags, base)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// moduleKeyFromSource keys p's module by the source of p and of all its
// dependencies, for a package the loader gave no ModBase.
func (b *build) moduleKeyFromSource(p *packages.Package) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\nseparate %s\n", b.srcKey(p), b.progFlags)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// srcKey hashes the source files of p and, through their keys, of its
// dependencies, with the flags and the marmot binary.
func (b *build) srcKey(p *packages.Package) string {
	b.keyMu.Lock()
	defer b.keyMu.Unlock()
	b.initKeyBase()
	return b.srcKeyLocked(p)
}

// initKeyBase sets keyBase, the hash of the flags and the marmot binary
// that every package key starts from. Called with keyMu held.
func (b *build) initKeyBase() {
	if b.keyMemo == nil {
		b.keyMemo = map[string]string{}
		exeSig := sha256.New()
		if exe, err := os.Executable(); err == nil {
			fileSig(exeSig, exe)
		}
		fmt.Fprintf(exeSig, "%s %s %s %s %v tier0=%v %s/%s\n", b.cfg.OptLevel, b.cfg.Emit, b.cfg.Dialect, b.cfg.GC, b.cfg.Release, b.tier0(), b.goos, b.goarch)
		for _, k := range []string{"MARMOT_LIBSRC", "MARMOT_DIALECT", "MARMOT_SPEC"} {
			fmt.Fprintf(exeSig, "%s=%s\n", k, os.Getenv(k))
		}
		writeNoFlags(exeSig)
		b.writeXDefs(exeSig)
		b.keyBase = hex.EncodeToString(exeSig.Sum(nil))
	}
}

func (b *build) srcKeyLocked(p *packages.Package) string {
	if k, ok := b.keyMemo[p.PkgPath]; ok {
		return k
	}
	b.keyMemo[p.PkgPath] = "" // cycle guard
	h := sha256.New()
	fmt.Fprintf(h, "pkg v2 %s %s\n", b.keyBase, p.PkgPath)
	// GoFiles: the full loader does not request CompiledGoFiles.
	files := append(append([]string{}, p.GoFiles...), p.CompiledGoFiles...)
	files = append(files, p.EmbedFiles...)
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
		fmt.Fprintf(h, "dep %s %s\n", d, b.srcKeyLocked(p.Imports[d]))
	}
	k := hex.EncodeToString(h.Sum(nil))[:32]
	b.keyMemo[p.PkgPath] = k
	return k
}

// cachedModule returns the cached MLIR file of the package with key k.
func (b *build) cachedModule(k string) (string, bool) {
	p := filepath.Join(b.cfg.Root, "build", "cache", "mlir", k+".mlir")
	if !fileExists(p) {
		return p, false
	}
	touch(p)
	return p, true
}

// saveModule stores the MLIR text of the package with key k and returns
// its path.
func (b *build) saveModule(k, text string) string {
	dir := filepath.Join(b.cfg.Root, "build", "cache", "mlir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "" // as a failed write: the caller compiles without the cache
	}
	p := filepath.Join(dir, k+".mlir")
	var head []byte
	if strings.HasPrefix(text, partsHeader) {
		// The parts line stays first (cachedParts reads it).
		n := strings.IndexByte(text, '\n') + 1
		head, text = []byte(text[:n]), text[n:]
	}
	if loader.WriteAtomic(p, appendAliasTypes(head, text)) == nil {
		return p
	}
	return ""
}

// writeNoFlags hashes every MARMOT_* environment variable that can change
// generated code: all of them except the few that only set paths, limits or
// run time behavior. A new switch is then part of every cache key without
// being listed here (a switch missing from a key once made cached objects
// built without it look like a passing run with it).
func writeNoFlags(w io.Writer) {
	var kv []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(k, "MARMOT_") && !noCodeEffect[k] && !strings.HasPrefix(k, "MARMOT_DEBUG_") {
			kv = append(kv, e)
		}
	}
	sort.Strings(kv)
	for _, e := range kv {
		fmt.Fprintln(w, e)
	}
}

// noCodeEffect are MARMOT_* variables that do not change generated code.
var noCodeEffect = map[string]bool{
	"MARMOT_ROOT": true, "MARMOT_MAX_JOBS": true, "MARMOT_CACHE_MAX_MB": true,
	"MARMOT_GOCACHE": true, "MARMOT_TOOL_TIMEOUT": true, "MARMOT_MAX_MEM_MB": true,
	"MARMOT_MAX_SECONDS": true, "MARMOT_SPEC_DUMP": true, "MARMOT_SPEC_DEBUG": true,
	"MARMOT_GPU": true, "MARMOT_GPUTRACE": true, "MARMOT_GPU_CACHE": true,
	// Background tier 1 only picks which of two equivalent objects links;
	// builds with it on or off share the cache.
	"MARMOT_TIER1": true,
}

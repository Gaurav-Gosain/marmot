package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Gaurav-Gosain/marmot/internal/codegen"
	"github.com/Gaurav-Gosain/marmot/internal/loader"
	"github.com/Gaurav-Gosain/marmot/internal/specialize"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// Incremental builds. A dev build parses, type checks and builds SSA only
// for the packages whose module is not in the MLIR cache; the others are
// imported from cached export data (loader.Config.Incremental) and their
// cached modules are reused. For that, the code generated for a package
// may not depend on whole-program facts (method pruning, dead globals,
// devirtualization): codegen.Options.Separate turns those off, and the
// module key covers only the package's sources, its dependencies' sources
// and the configuration (moduleKey).
//
//	MARMOT_INCREMENTAL=0  load every package from source

// errIncrementalMiss reports that the module of a package loaded without
// syntax was not in the cache after all (removed during the build).
var errIncrementalMiss = errors.New("incremental: cached module missing")

// incremental reports whether this build may load packages from export
// data.
func (b *build) incremental() bool {
	if b.noIncr || os.Getenv("MARMOT_INCREMENTAL") == "0" {
		return false
	}
	c := b.cfg
	return !c.Release && !b.wpo() && c.Dialect != "go" && !c.Test && !c.NoCache &&
		(c.Emit == "exe" || c.Emit == "obj") && !specialize.Enabled(false)
}

// progFlagsPath holds the program flags of the last incremental build of
// this configuration.
func (b *build) progFlagsPath() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%q\n%s/%s\n", b.cfg.Dir, b.cfg.Patterns, b.goos, b.goarch)
	return filepath.Join(b.typesCache(), "flags-"+hex.EncodeToString(h.Sum(nil))[:24])
}

// lastProgFlags returns the program flags the previous build found.
func (b *build) lastProgFlags() string {
	data, err := os.ReadFile(b.progFlagsPath())
	if err != nil {
		return "unknown"
	}
	return string(data)
}

// progFlags computes the program flags from every package's facts.
func progFlags(lp *loader.Program) (s string, keepExported, reflectCall bool) {
	for _, ip := range lp.Incremental {
		if f := ip.Facts; f != nil {
			keepExported = keepExported || f.MethodLookup
			reflectCall = reflectCall || f.ReflectCall
		}
	}
	return fmt.Sprintf("ke=%v rc=%v", keepExported, reflectCall), keepExported, reflectCall
}

// checkProgFlags sets the program flags of the loaded program. When they
// are not those the load assumed, packages loaded from export data may lack
// a module under the new keys: checkIncremental then fails and the build
// runs again.
func (b *build) checkProgFlags(lp *loader.Program) (keepExported, reflectCall bool) {
	s, ke, rc := progFlags(lp)
	if s != b.progFlags {
		b.logf("program flags %s (assumed %s)", s, b.progFlags)
		if err := os.MkdirAll(b.typesCache(), 0o755); err != nil {
			b.logf("program flags: %v", err)
		} else if err := loader.WriteAtomic(b.progFlagsPath(), []byte(s)); err != nil {
			b.logf("program flags: %v", err)
		}
		b.progFlags = s
	}
	return ke, rc
}

// moduleCached reports whether the module of p is in the MLIR cache.
// moduleCached reports whether the module of p, whose loader ModBase is
// base, is in the MLIR cache.
func (b *build) moduleCached(p *packages.Package, base string) bool {
	if p.PkgPath == "command-line-arguments" || base == "" {
		return false
	}
	k := b.moduleKeyFromSource(p)
	if apiKeys {
		k = b.moduleKey(base)
	}
	return fileExists(filepath.Join(b.cfg.Root, "build", "cache", "mlir", k+".mlir"))
}

// typesCache is the export data and go list cache of incremental loads.
func (b *build) typesCache() string { return filepath.Join(b.cfg.Root, "build", "cache", "types") }

// incrementalConfig sets up cfg for an incremental load.
func (b *build) incrementalConfig(cfg *loader.Config) {
	cfg.Incremental = true
	b.progFlags = b.lastProgFlags()
	cfg.CacheDir = b.typesCache()
	// Keys come from the go list graph, before the loader adds files
	// (embed helpers) to packages loaded from source.
	cfg.Listed = func(all []*packages.Package) {
		for _, p := range all {
			b.srcKey(p)
		}
	}
	// Code generation looks up runtime functions and types by name,
	// unexported ones included, which export data leaves out.
	cfg.NeedSource = func(p *packages.Package, base string) bool {
		return p.PkgPath == "runtime" || !b.moduleCached(p, base)
	}
}

// incrementalFacts returns the facts of the packages loaded without syntax.
func incrementalFacts(lp *loader.Program) map[string]*loader.Facts {
	if lp.Incremental == nil {
		return nil
	}
	out := map[string]*loader.Facts{}
	for path, ip := range lp.Incremental {
		if !ip.FromSource && ip.Facts != nil {
			out[path] = ip.Facts
		}
	}
	return out
}

// checkIncremental fails with errIncrementalMiss when a package loaded
// without syntax has no module (its cached module was removed).
func (b *build) checkIncremental(lp *loader.Program, pkgs []*ssa.Package, mods []codegen.Module) error {
	if lp.Incremental == nil {
		return nil
	}
	for i, sp := range pkgs {
		if mods[i].Path != "" || mods[i].Text != "" {
			continue
		}
		if ip := lp.Incremental[sp.Pkg.Path()]; ip != nil && !ip.FromSource {
			return fmt.Errorf("%w: %s", errIncrementalMiss, sp.Pkg.Path())
		}
	}
	return nil
}

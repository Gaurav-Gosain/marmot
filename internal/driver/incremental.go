package driver

import (
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
	if b.noIncr || os.Getenv("MARMOT_INCREMENTAL") != "1" {
		return false
	}
	c := b.cfg
	return !c.Release && !b.wpo() && c.Dialect != "go" && !c.Test && !c.NoCache &&
		(c.Emit == "exe" || c.Emit == "obj") && !specialize.Enabled(false)
}

// moduleCached reports whether the module of p is in the MLIR cache.
func (b *build) moduleCached(p *packages.Package) bool {
	if p.PkgPath == "command-line-arguments" {
		return false
	}
	return fileExists(filepath.Join(b.cfg.Root, "build", "cache", "mlir", b.moduleKey(p)+".mlir"))
}

// incrementalConfig sets up cfg for an incremental load.
func (b *build) incrementalConfig(cfg *loader.Config) {
	cfg.Incremental = true
	cfg.CacheDir = filepath.Join(b.cfg.Root, "build", "cache", "types")
	// Keys come from the go list graph, before the loader adds files
	// (embed helpers) to packages loaded from source.
	cfg.Listed = func(all []*packages.Package) {
		for _, p := range all {
			b.srcKey(p)
		}
	}
	cfg.NeedSource = func(p *packages.Package, key string) bool { return !b.moduleCached(p) }
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

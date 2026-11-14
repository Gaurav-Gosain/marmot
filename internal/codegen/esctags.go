package codegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"

	"golang.org/x/tools/go/ssa"
)

// Escape tags: per function, which parameters may escape it, as gc's
// parameter tags in export data. In separate compilation an analysis may
// not read another package's bodies (sameUnit), so a value passed to a
// function of another package used to escape. Each package now computes
// tags for its functions from its own bodies, treating calls into other
// packages as escaping (one level: the tags depend on the package's source
// and the types it imports only), and its importers read those tags.
//
// A tag changes code of every importer, so it is part of their module key
// (driver/stamp.go), and the driver caches the tags of a package next to
// its export data, keyed by the same key.
//
// On with MARMOT_ESCAPE_SUMMARY=1 MARMOT_XPKG_ESCAPE=1 (lifetime switches).
// MARMOT_CHECK_TAGS=1 makes a missing tag of a function in a package that
// has tags a build failure (tests).

// EscTags maps a function's symbol to its tag: bit i set when parameter i
// may escape; bit 32+i set when parameter i leaves only through the
// results. Functions with more than 32 parameters get no tag.
type EscTags map[string]uint64

// EscTagsOn reports whether builds use escape tags.
func EscTagsOn() bool { return optEscSummary && optXPkgEscape }

var checkTags = os.Getenv("MARMOT_CHECK_TAGS") == "1"

// ComputeEscTags returns the tags of the functions pkg owns. Call it for
// every package before SetImportTags: the analysis then sees no other
// package's tags, so the result depends only on pkg's own code.
func (p *Program) ComputeEscTags(pkg *ssa.Package) EscTags {
	if p.importTags != nil {
		panic("codegen: ComputeEscTags after SetImportTags")
	}
	out := EscTags{}
	for _, fn := range p.byPkg[pkg] {
		// Body-less functions (assembly, the C core) are tagged too:
		// paramEscapes knows them as escaping unless //go:noescape.
		// Parameters as call arguments count them: the receiver first. A
		// body-less function has no Params, only its signature.
		n := fn.Signature.Params().Len()
		if fn.Signature.Recv() != nil {
			n++
		}
		if n > 32 || fn.TypeParams().Len() > 0 {
			continue
		}
		var t uint64
		for i := 0; i < n; i++ {
			if p.paramEscapes(fn, i) {
				t |= 1 << i
				if optEscapeExt && p.paramToResult(fn, i) {
					t |= 1 << (32 + i)
				}
			}
		}
		out[p.symbol(fn)] = t
	}
	return out
}

// Hash is a stable digest of the tags (module keys of importers).
func (t EscTags) Hash() string {
	if t == nil {
		return "none"
	}
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s %x\n", k, t[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// SetImportTags gives the program every package's tags (nil: unknown) and
// starts using them. The memoized summaries computed without them are
// dropped.
func (p *Program) SetImportTags(tags map[*ssa.Package]EscTags) {
	p.escMu.Lock()
	p.esc = escapeState{}
	p.escMu.Unlock()
	p.importTags = tags
}

// importTag returns fn's tag from its package's tags.
func (p *Program) importTag(fn *ssa.Function) (uint64, bool) {
	if p.importTags == nil {
		return 0, false
	}
	o := p.owner[fn]
	if o == nil {
		return 0, false
	}
	tags := p.importTags[o]
	if tags == nil {
		return 0, false
	}
	t, ok := tags[p.symbol(fn)]
	if !ok && checkTags && fn.Signature.Params().Len() < 32 && fn.TypeParams().Len() == 0 {
		panic(fmt.Sprintf("codegen: no escape tag for %s in the tags of %s", p.symbol(fn), o.Pkg.Path()))
	}
	return t, ok
}

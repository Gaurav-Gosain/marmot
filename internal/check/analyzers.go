package check

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/Gaurav-Gosain/marmot/internal/check/unused"
	"github.com/gordonklaus/ineffassign/pkg/ineffassign"
	"github.com/kisielk/errcheck/errcheck"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/appends"
	"golang.org/x/tools/go/analysis/passes/asmdecl"
	"golang.org/x/tools/go/analysis/passes/assign"
	"golang.org/x/tools/go/analysis/passes/atomic"
	"golang.org/x/tools/go/analysis/passes/atomicalign"
	"golang.org/x/tools/go/analysis/passes/bools"
	"golang.org/x/tools/go/analysis/passes/buildtag"
	"golang.org/x/tools/go/analysis/passes/cgocall"
	"golang.org/x/tools/go/analysis/passes/composite"
	"golang.org/x/tools/go/analysis/passes/copylock"
	"golang.org/x/tools/go/analysis/passes/deepequalerrors"
	"golang.org/x/tools/go/analysis/passes/defers"
	"golang.org/x/tools/go/analysis/passes/directive"
	"golang.org/x/tools/go/analysis/passes/errorsas"
	"golang.org/x/tools/go/analysis/passes/fieldalignment"
	"golang.org/x/tools/go/analysis/passes/findcall"
	"golang.org/x/tools/go/analysis/passes/framepointer"
	"golang.org/x/tools/go/analysis/passes/hostport"
	"golang.org/x/tools/go/analysis/passes/httpmux"
	"golang.org/x/tools/go/analysis/passes/httpresponse"
	"golang.org/x/tools/go/analysis/passes/ifaceassert"
	"golang.org/x/tools/go/analysis/passes/inline"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/passes/loopclosure"
	"golang.org/x/tools/go/analysis/passes/lostcancel"
	"golang.org/x/tools/go/analysis/passes/nilfunc"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/analysis/passes/reflectvaluecompare"
	"golang.org/x/tools/go/analysis/passes/shadow"
	"golang.org/x/tools/go/analysis/passes/shift"
	"golang.org/x/tools/go/analysis/passes/sigchanyzer"
	"golang.org/x/tools/go/analysis/passes/slog"
	"golang.org/x/tools/go/analysis/passes/sortslice"
	"golang.org/x/tools/go/analysis/passes/stdmethods"
	"golang.org/x/tools/go/analysis/passes/stdversion"
	"golang.org/x/tools/go/analysis/passes/stringintconv"
	"golang.org/x/tools/go/analysis/passes/structtag"
	"golang.org/x/tools/go/analysis/passes/testinggoroutine"
	"golang.org/x/tools/go/analysis/passes/tests"
	"golang.org/x/tools/go/analysis/passes/timeformat"
	"golang.org/x/tools/go/analysis/passes/unmarshal"
	"golang.org/x/tools/go/analysis/passes/unreachable"
	"golang.org/x/tools/go/analysis/passes/unsafeptr"
	"golang.org/x/tools/go/analysis/passes/unusedresult"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"golang.org/x/tools/go/analysis/passes/waitgroup"
	"golang.org/x/tools/go/analysis/suite/fix"
	"golang.org/x/tools/go/analysis/suite/vet"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/analysis/lint"
	scconfig "honnef.co/go/tools/config"
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
)

// family is the analyzers behind one check, with the linter name each
// finding is reported under (golangci-lint's names for lint).
type family struct {
	analyzers []*analysis.Analyzer
	linter    map[*analysis.Analyzer]string
	// notes are said once in the check's output (unsupported settings).
	notes []string
}

func (f *family) add(linter string, as ...*analysis.Analyzer) {
	if f.linter == nil {
		f.linter = map[*analysis.Analyzer]string{}
	}
	for _, a := range as {
		if _, ok := f.linter[a]; ok {
			continue
		}
		f.analyzers = append(f.analyzers, a)
		f.linter[a] = linter
	}
}

// vetFamily is go vet with its default analyzers.
func vetFamily() *family {
	f := &family{}
	f.add("vet", vet.Suite...)
	return f
}

// modernizeFamily is go fix: the modernizers, the //go:fix inline
// inliner and the other fixers, each version aware.
func modernizeFamily() *family {
	f := &family{}
	f.add("modernize", fix.Suite...)
	return f
}

// govetAll is every analyzer golangci-lint's govet linter knows, and
// govetDefault the ones it runs by default (go vet's set).
var govetAll = []*analysis.Analyzer{
	appends.Analyzer, asmdecl.Analyzer, assign.Analyzer, atomic.Analyzer, atomicalign.Analyzer,
	bools.Analyzer, buildtag.Analyzer, cgocall.Analyzer, composite.Analyzer, copylock.Analyzer,
	deepequalerrors.Analyzer, defers.Analyzer, directive.Analyzer, errorsas.Analyzer,
	fieldalignment.Analyzer, findcall.Analyzer, framepointer.Analyzer, hostport.Analyzer,
	httpmux.Analyzer, httpresponse.Analyzer, ifaceassert.Analyzer, inline.Analyzer,
	loopclosure.Analyzer, lostcancel.Analyzer, nilfunc.Analyzer, nilness.Analyzer, printf.Analyzer,
	reflectvaluecompare.Analyzer, shadow.Analyzer, shift.Analyzer, sigchanyzer.Analyzer,
	slog.Analyzer, sortslice.Analyzer, stdmethods.Analyzer, stdversion.Analyzer,
	stringintconv.Analyzer, structtag.Analyzer, testinggoroutine.Analyzer, tests.Analyzer,
	timeformat.Analyzer, unmarshal.Analyzer, unreachable.Analyzer, unsafeptr.Analyzer,
	unusedresult.Analyzer, unusedwrite.Analyzer, waitgroup.Analyzer,
}

var govetDefault = append(slices.Clone(vet.Suite), inline.Analyzer)

// standardLinters is golangci-lint's "standard" default set.
var standardLinters = []string{"errcheck", "govet", "ineffassign", "staticcheck", "unused"}

// knownLinters are the golangci-lint linters marmot check runs.
var knownLinters = map[string]bool{
	"errcheck": true, "govet": true, "ineffassign": true, "staticcheck": true, "unused": true,
	"gosec": true, "modernize": true,
}

// lintFamily is golangci-lint's linter set for a configuration (nil means
// golangci-lint's defaults).
func lintFamily(c *lintConfig, moduleGo string) *family {
	f := &family{}
	var s lintSettings
	enabled := map[string]bool{}
	def := "standard"
	if c != nil {
		s = c.Linters.Settings
		if c.Linters.Default != "" {
			def = c.Linters.Default
		}
	}
	switch def {
	case "standard":
		for _, l := range standardLinters {
			enabled[l] = true
		}
	case "all":
		for l := range knownLinters {
			enabled[l] = true
		}
		f.notes = append(f.notes, "linters.default: all runs only the linters marmot has: "+strings.Join(sortedKeys(knownLinters), ", "))
	case "none":
	case "fast":
		enabled["ineffassign"] = true
		f.notes = append(f.notes, "linters.default: fast runs only ineffassign")
	}
	if c != nil {
		for _, l := range c.Linters.Enable {
			if !knownLinters[l] {
				f.notes = append(f.notes, fmt.Sprintf("linter %s is not available in marmot check; it is skipped", l))
				continue
			}
			enabled[l] = true
		}
		for _, l := range c.Linters.Disable {
			delete(enabled, l)
		}
	}
	if enabled["govet"] {
		gs := s.Govet
		var defaults []*analysis.Analyzer
		for _, a := range govetAll {
			if !slices.Contains(govetDefault, a) {
				continue
			}
			defaults = append(defaults, a)
		}
		for _, a := range govetAll {
			name := a.Name
			on := false
			switch {
			case name == loopclosure.Analyzer.Name && goAtLeast(moduleGo, "1.22"):
				on = false
			case gs.EnableAll:
				on = !slices.Contains(gs.Disable, name)
			case slices.Contains(gs.Enable, name):
				on = true
			case slices.Contains(gs.Disable, name):
				on = false
			case gs.DisableAll:
				on = false
			default:
				on = slices.Contains(defaults, a)
			}
			if on {
				f.add("govet", a)
			}
		}
	}
	if enabled["staticcheck"] {
		cfg := staticcheckConfig(s)
		scconfig.Analyzer.Run = func(*analysis.Pass) (any, error) { return cfg, nil }
		all := slices.Concat(staticcheck.Analyzers, stylecheck.Analyzers, simple.Analyzers, quickfix.Analyzers)
		names := make([]string, 0, len(all))
		for _, a := range all {
			names = append(names, a.Analyzer.Name)
		}
		on := filterChecks(names, cfg.Checks)
		for _, a := range all {
			if on[a.Analyzer.Name] {
				f.add("staticcheck", a.Analyzer)
			}
		}
	}
	if enabled["errcheck"] {
		f.add("errcheck", errcheckAnalyzer(s))
	}
	if enabled["ineffassign"] {
		f.add("ineffassign", ineffassign.Analyzer)
	}
	if enabled["unused"] {
		f.add("unused", unusedAnalyzer(s))
	}
	if enabled["gosec"] {
		a, notes := gosecAnalyzer(s)
		f.add("gosec", a)
		f.notes = append(f.notes, notes...)
	}
	if enabled["modernize"] {
		for _, a := range modernizeFamily().analyzers {
			f.add("modernize", a)
		}
	}
	return f
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// goAtLeast compares Go versions like "1.22" or "1.22.3".
func goAtLeast(v, min string) bool {
	if v == "" {
		return true
	}
	a, b := strings.Split(v, "."), strings.Split(min, ".")
	for i := range b {
		if i >= len(a) {
			return false
		}
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x > y
		}
	}
	return true
}

func staticcheckConfig(s lintSettings) *scconfig.Config {
	st := s.Staticcheck
	def := []string{"all", "-ST1000", "-ST1003", "-ST1016", "-ST1020", "-ST1021", "-ST1022"}
	cfg := &scconfig.Config{
		Checks:                  orList(st.Checks, def),
		Initialisms:             orList(st.Initialisms, scconfig.DefaultConfig.Initialisms),
		DotImportWhitelist:      orList(st.DotImportWhitelist, scconfig.DefaultConfig.DotImportWhitelist),
		HTTPStatusCodeWhitelist: orList(st.HTTPStatusCodeWhitelist, scconfig.DefaultConfig.HTTPStatusCodeWhitelist),
	}
	return cfg
}

func orList(a, b []string) []string {
	if a != nil {
		return a
	}
	return b
}

// filterChecks applies a staticcheck checks list ("all", "-ST1000",
// "S1*", "SA*") to the check names, as golangci-lint does.
func filterChecks(names, checks []string) map[string]bool {
	on := map[string]bool{}
	for _, c := range checks {
		b := true
		if len(c) > 1 && c[0] == '-' {
			b, c = false, c[1:]
		}
		switch {
		case c == "*" || c == "all":
			for _, n := range names {
				on[n] = b
			}
		case strings.HasSuffix(c, "*"):
			prefix := c[:len(c)-1]
			isCat := strings.IndexFunc(prefix, unicode.IsNumber) == -1
			for _, n := range names {
				if isCat {
					i := strings.IndexFunc(n, unicode.IsNumber)
					if i >= 0 && n[:i] == prefix {
						on[n] = b
					}
				} else if strings.HasPrefix(n, prefix) {
					on[n] = b
				}
			}
		default:
			on[c] = b
		}
	}
	return on
}

// errcheckAnalyzer runs errcheck with golangci-lint's settings and
// message format.
func errcheckAnalyzer(s lintSettings) *analysis.Analyzer {
	es := s.Errcheck
	checker := &errcheck.Checker{
		Exclusions: errcheck.Exclusions{
			BlankAssignments:       !es.CheckBlank,
			TypeAssertions:         !es.CheckTypeAssertions,
			SymbolRegexpsByPackage: map[string]*regexp.Regexp{},
		},
	}
	if !es.DisableDefaultExclusions {
		checker.Exclusions.Symbols = append(checker.Exclusions.Symbols, errcheck.DefaultExcludedSymbols...)
	}
	checker.Exclusions.Symbols = append(checker.Exclusions.Symbols, es.ExcludeFunctions...)
	return &analysis.Analyzer{
		Name: "errcheck",
		Doc:  "check for unchecked errors",
		Run: func(pass *analysis.Pass) (any, error) {
			pkg := &packages.Package{Fset: pass.Fset, Syntax: pass.Files, Types: pass.Pkg, TypesInfo: pass.TypesInfo}
			res := checker.CheckPackage(pkg).Unique()
			for _, u := range res.UncheckedErrors {
				text := "Error return value is not checked"
				if u.FuncName != "" {
					text = fmt.Sprintf("Error return value of `%s` is not checked", cmp.Or(u.SelectorName, u.FuncName))
				}
				if pos := posOf(pass, u.Pos); pos.IsValid() {
					pass.Report(analysis.Diagnostic{Pos: pos, Message: text})
				}
			}
			return nil, nil
		},
	}
}

// unusedAnalyzer runs staticcheck's unused with golangci-lint's options
// and message format.
func unusedAnalyzer(s lintSettings) *analysis.Analyzer {
	us := s.Unused
	or := func(p *bool) bool { return p == nil || *p }
	opts := unused.Options{
		FieldWritesAreUses:     or(us.FieldWritesAreUses),
		PostStatementsAreReads: us.PostStatementsAreReads,
		ExportedIsUsed:         true,
		ExportedFieldsAreUsed:  or(us.ExportedFieldsAreUsed),
		ParametersAreUsed:      or(us.ParametersAreUsed),
		LocalVariablesAreUsed:  or(us.LocalVariablesAreUsed),
		GeneratedIsUsed:        or(us.GeneratedIsUsed),
	}
	// unused's own analyzer reads its options from this variable; its
	// result needs no merging for a single package (Merge prints a trace
	// on stderr).
	unused.DefaultOptions = opts
	return &analysis.Analyzer{
		Name:     "unused",
		Doc:      unused.Analyzer.Analyzer.Doc,
		Requires: []*analysis.Analyzer{unused.Analyzer.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			res := pass.ResultOf[unused.Analyzer.Analyzer].(unused.Result)
			used := map[string]bool{}
			for _, o := range res.Used {
				used[fmt.Sprintf("%s %d %s", o.Position.Filename, o.Position.Line, o.Name)] = true
			}
			for _, o := range res.Unused {
				if o.Kind == "type param" || used[fmt.Sprintf("%s %d %s", o.Position.Filename, o.Position.Line, o.Name)] {
					continue
				}
				if pos := posOf(pass, o.Position); pos.IsValid() {
					pass.Report(analysis.Diagnostic{Pos: pos, Message: fmt.Sprintf("%s %s is unused", o.Kind, o.Name)})
				}
			}
			return nil, nil
		},
	}
}

// posOf turns a position back into a token.Pos of the pass's files.
func posOf(pass *analysis.Pass, p token.Position) token.Pos {
	for _, f := range pass.Files {
		tf := pass.Fset.File(f.FileStart)
		if tf.Name() == p.Filename && p.Offset <= tf.Size() {
			return tf.Pos(p.Offset)
		}
	}
	return token.NoPos
}

// gosec rules marmot implements: the file permission rules.
var gosecRules = map[string]struct {
	calls []string // pkg.Func
	mode  int64
	what  string
}{
	"G301": {[]string{"os.Mkdir", "os.MkdirAll"}, 0o750, "Expect directory permissions to be %#o or less"},
	"G302": {[]string{"os.OpenFile", "os.Chmod"}, 0o600, "Expect file permissions to be %#o or less"},
	"G306": {[]string{"os.WriteFile", "io/ioutil.WriteFile"}, 0o600, "Expect WriteFile permissions to be %#o or less"},
	"G307": {[]string{"os.Create"}, 0o666, "Expect file permissions to be %#o or less but os.Create used with default permissions 0666"},
}

func gosecAnalyzer(s lintSettings) (*analysis.Analyzer, []string) {
	var notes []string
	ids := s.Gosec.Includes
	if len(ids) == 0 {
		for id := range gosecRules {
			ids = append(ids, id)
		}
		notes = append(notes, "gosec: only the file permission rules (G301, G302, G306, G307) are implemented")
	}
	var rules []string
	for _, id := range ids {
		if slices.Contains(s.Gosec.Excludes, id) {
			continue
		}
		if _, ok := gosecRules[id]; !ok {
			notes = append(notes, "gosec rule "+id+" is not implemented in marmot check; it is skipped")
			continue
		}
		rules = append(rules, id)
	}
	sort.Strings(rules)
	return &analysis.Analyzer{
		Name:     "gosec",
		Doc:      "file permission rules of gosec",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
			nosec := nosecRanges(pass)
			ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
				call := n.(*ast.CallExpr)
				name := calleeName(pass.TypesInfo, call)
				if name == "" {
					return
				}
				for _, id := range rules {
					r := gosecRules[id]
					if !slices.Contains(r.calls, name) {
						continue
					}
					if id == "G307" {
						if (0o666|r.mode) != r.mode && !nosec.covers(call.Pos(), id) {
							pass.Report(analysis.Diagnostic{Pos: call.Pos(), End: call.End(), Message: id + ": " + fmt.Sprintf(r.what, r.mode)})
						}
						continue
					}
					if len(call.Args) == 0 {
						continue
					}
					arg := call.Args[len(call.Args)-1]
					bad := isOsModePerm(arg)
					if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.INT {
						if m, err := strconv.ParseInt(lit.Value, 0, 64); err == nil && (m|r.mode) != r.mode {
							bad = true
						}
					}
					if bad && !nosec.covers(call.Pos(), id) {
						pass.Report(analysis.Diagnostic{Pos: call.Pos(), End: call.End(), Message: id + ": " + fmt.Sprintf(r.what, r.mode)})
					}
				}
			})
			return nil, nil
		},
	}, notes
}

func isOsModePerm(n ast.Expr) bool {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "os" && sel.Sel.Name == "ModePerm"
}

// calleeName returns "pkgpath.Func" for a call of a package level
// function.
func calleeName(info *types.Info, call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	fn, ok := info.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return ""
	}
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		return ""
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// nosecSet holds the nodes that a gosec #nosec comment covers.
type nosecSet []nosecRange

type nosecRange struct {
	pos, end token.Pos
	rules    []string // none means every rule
}

var reNosec = regexp.MustCompile(`#nosec\b([^-/]*)`)
var reRuleID = regexp.MustCompile(`G\d{3}`)

// nosecRanges finds #nosec comments the way gosec reads them: a comment
// associated with a node (ast.CommentMap) covers that node and everything
// in it, for the rules it names or for all.
func nosecRanges(pass *analysis.Pass) nosecSet {
	var out nosecSet
	for _, f := range pass.Files {
		cm := ast.NewCommentMap(pass.Fset, f, f.Comments)
		for n, groups := range cm {
			for _, g := range groups {
				m := reNosec.FindStringSubmatch(g.Text())
				if m == nil {
					continue
				}
				out = append(out, nosecRange{n.Pos(), n.End(), reRuleID.FindAllString(m[1], -1)})
			}
		}
	}
	return out
}

func (s nosecSet) covers(pos token.Pos, rule string) bool {
	for _, r := range s {
		if pos >= r.pos && pos < r.end && (len(r.rules) == 0 || slices.Contains(r.rules, rule)) {
			return true
		}
	}
	return false
}

// goplsDefaultStaticcheck are the staticcheck checks gopls enables by
// default (gopls v0.24 settings).
var goplsDefaultStaticcheck = strings.Fields(`S1000 S1001 S1003 S1004 S1007 S1009 S1010 S1012 S1017 S1018 S1019 S1020
S1023 S1024 S1028 S1030 S1031 S1032 S1033 S1034 S1035 S1036 S1037 S1038 S1039 S1040 QF1002 QF1003
QF1004 QF1009 QF1010 QF1012 SA1001 SA1004 SA1005 SA1008 SA1012 SA1013 SA1016 SA2001 SA3000 SA3001
SA4000 SA4001 SA4003 SA4004 SA4011 SA4013 SA4014 SA4016 SA4019 SA4020 SA4022 SA4024 SA4025 SA4026
SA4027 SA4028 SA4029 SA4030 SA4032 SA5001 SA5003 SA5004 SA5008 SA6005 SA6006 SA9002 SA9004 SA9006
SA9009 SA9010`)

// goplsFamily is what gopls check prints: diagnostics of warning severity
// or more, from the vet suite, the gopls analyzers that are on by default
// at that severity and are public, and its default staticcheck subset
// (those of information or hint severity, such as the gofmt -s
// simplifiers, are not printed by gopls check).
func goplsFamily() *family {
	f := &family{}
	f.add("gopls", vet.Suite...)
	f.add("gopls", atomicalign.Analyzer, deepequalerrors.Analyzer, nilness.Analyzer, sortslice.Analyzer)
	on := map[string]bool{}
	for _, n := range goplsDefaultStaticcheck {
		on[n] = true
	}
	for _, a := range slices.Concat(staticcheck.Analyzers, stylecheck.Analyzers, simple.Analyzers, quickfix.Analyzers) {
		if !on[a.Analyzer.Name] {
			continue
		}
		switch a.Doc.Severity {
		case lint.SeverityInfo, lint.SeverityHint:
			continue
		}
		f.add("gopls", a.Analyzer)
	}
	f.notes = append(f.notes, "not included: gopls analyzers internal to gopls (yield, embeddirective, recursiveiter, errorsastypeshadow, writestring, ptrtoerror)")
	return f
}

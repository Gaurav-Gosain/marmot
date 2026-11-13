package check

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

// FixOptions configure marmot fix for analyzer findings.
type FixOptions struct {
	Options
	All     bool   // also maybe-incorrect fixes (vet, lint, exhaustive)
	Diff    bool   // print the changes as patches, change nothing
	Test    bool   // the tests of changed packages must pass too
	Rounds  int    // at most this many rounds (default 5)
	Upgrade string // raise the go version of the modules first ("1.26" or "go1.26")
}

// Applied is one fix that was written (or, with Diff, would be).
type Applied struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Check    string `json:"check"`
	Analyzer string `json:"analyzer"`
	Title    string `json:"title"`
}

// FixResult is the outcome of marmot fix.
type FixResult struct {
	Applied    []Applied
	RolledBack []Applied // fixes undone because the tree stopped type checking or a test failed
	Conflicts  int       // fixes skipped because they overlap a fix applied in the same round
	Diff       string    // with Diff, the patches
	Notes      []string
	Rounds     int
}

// DefaultFixChecks are the checks whose fixes marmot fix applies by
// default: their fixes do not change behavior.
var DefaultFixChecks = []string{"fmt", "modernize"}

// Fix applies the machine applicable fixes of the checks, in rounds, until
// none applies. After each round the changed packages and their importers
// are type checked again (the cache makes that cheap); when a package no
// longer type checks, or with Test its tests fail, the files of that round
// are restored and the round's fixes are retried one package at a time,
// keeping only those that verify.
func Fix(fo FixOptions) (*FixResult, error) {
	if len(fo.Only) == 0 {
		fo.Only = DefaultFixChecks
	}
	if fo.Rounds <= 0 {
		fo.Rounds = 5
	}
	if fo.Diff {
		fo.Rounds = 1
	}
	res := &FixResult{}
	dir, err := filepath.Abs(cmpOr(fo.Dir, "."))
	if err != nil {
		return nil, err
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	fo.Dir = dir
	if fo.Upgrade != "" && !fo.Diff {
		v := strings.TrimPrefix(fo.Upgrade, "go")
		mods, err := workspaceModules(dir)
		if err != nil {
			return nil, err
		}
		for _, m := range mods {
			if _, err := goCmd(m.Dir, []string{"GOWORK=off"}, "mod", "edit", "-go="+v); err != nil {
				return nil, err
			}
			res.Notes = append(res.Notes, fmt.Sprintf("%s: go %s", diag.RelPath(dir, filepath.Join(m.Dir, "go.mod")), v))
		}
	}
	// The tree must type check before anything is changed, so that a
	// failure afterwards is the fixes' doing.
	before, err := typeErrors(fo.Options)
	if err != nil {
		return nil, err
	}
	if len(before) > 0 {
		return nil, fmt.Errorf("the packages do not type check (%s); fix the errors first (marmot build shows them)", before[0])
	}
	for round := 0; round < fo.Rounds; round++ {
		r, err := Run(fo.Options)
		if err != nil {
			return nil, err
		}
		cands := candidates(r, fo.All)
		if len(cands) == 0 {
			break
		}
		res.Rounds++
		chosen, conflicts := selectFixes(cands)
		res.Conflicts = conflicts
		files, err := applyEdits(chosen)
		if err != nil {
			return nil, err
		}
		if fo.Diff {
			res.Diff = diffFiles(files, dir)
			res.Applied = append(res.Applied, appliedOf(chosen)...)
			break
		}
		writeFiles(files, true)
		if ok, _ := verify(fo, files); ok {
			res.Applied = append(res.Applied, appliedOf(chosen)...)
			if conflicts == 0 {
				// Chained fixes may still appear: one more round finds them.
				continue
			}
			continue
		}
		// Undo the round, then keep the fixes of one directory at a time
		// when they verify.
		writeFiles(files, false)
		byDir := map[string][]candidate{}
		var dirs []string
		for _, c := range chosen {
			d := filepath.Dir(c.d.Span.File)
			if byDir[d] == nil {
				dirs = append(dirs, d)
			}
			byDir[d] = append(byDir[d], c)
		}
		sort.Strings(dirs)
		progress := false
		for _, d := range dirs {
			fs, err := applyEdits(byDir[d])
			if err != nil {
				return nil, err
			}
			writeFiles(fs, true)
			if ok, _ := verify(fo, fs); ok {
				res.Applied = append(res.Applied, appliedOf(byDir[d])...)
				progress = true
				continue
			}
			writeFiles(fs, false)
			res.RolledBack = append(res.RolledBack, appliedOf(byDir[d])...)
		}
		if !progress {
			break
		}
	}
	return res, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type candidate struct {
	d     diag.Diagnostic
	edits []diag.Edit
}

// candidates are the findings with a fix that may be applied.
func candidates(r *Result, all bool) []candidate {
	var out []candidate
	for _, c := range r.Checks {
		for _, d := range c.Findings {
			if len(d.Fixes) == 0 {
				continue
			}
			fx := d.Fixes[0]
			if fx.Applicability != diag.MachineApplicable && (!all || fx.Applicability != diag.MaybeIncorrect) {
				continue
			}
			var es []diag.Edit
			seen := map[string]bool{}
			for _, e := range fx.Edits {
				empty := e.NewText == "" && e.Span.EndLine == e.Span.Line && e.Span.EndCol == e.Span.Col
				k := fmt.Sprint(e.File, e.Span, e.NewText)
				if empty || seen[k] {
					continue // no-op edits some analyzers emit
				}
				seen[k] = true
				es = append(es, e)
			}
			if len(es) > 0 {
				out = append(out, candidate{d, es})
			}
		}
	}
	return out
}

// fileEdit is an edit in byte offsets.
type fileEdit struct {
	a, b int
	text string
}

type fileState struct {
	path     string
	old, new []byte
}

// selectFixes keeps fixes whose edits overlap no fix taken before them
// (in position order) and returns how many it skipped.
func selectFixes(cs []candidate) ([]candidate, int) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i].d.Span, cs[j].d.Span
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	type rng struct {
		file   string
		l1, c1 int
		l2, c2 int
	}
	before := func(l1, c1, l2, c2 int) bool { return l1 < l2 || l1 == l2 && c1 < c2 }
	var taken []rng
	var out []candidate
	skipped := 0
	for _, c := range cs {
		ok := true
		for _, e := range c.edits {
			for _, t := range taken {
				if t.file != e.File {
					continue
				}
				// overlap unless one ends before the other starts
				if before(e.Span.EndLine, e.Span.EndCol, t.l1, t.c1) || before(t.l2, t.c2, e.Span.Line, e.Span.Col) {
					continue
				}
				if e.Span.EndLine == t.l1 && e.Span.EndCol == t.c1 && (e.Span.Line != t.l1 || e.Span.Col != t.c1) {
					continue // touching, not overlapping
				}
				if t.l2 == e.Span.Line && t.c2 == e.Span.Col && (t.l1 != t.l2 || t.c1 != t.c2) {
					continue
				}
				ok = false
			}
		}
		if !ok {
			skipped++
			continue
		}
		for _, e := range c.edits {
			taken = append(taken, rng{e.File, e.Span.Line, e.Span.Col, e.Span.EndLine, e.Span.EndCol})
		}
		out = append(out, c)
	}
	return out, skipped
}

// applyEdits computes the new contents of every file the fixes touch.
// reformat runs gofmt over a fixed Go file that was formatted before:
// suggested fixes (a tagged switch, a rewritten condition) are not always
// formatted text.
func reformat(old, fixed []byte) []byte {
	if formatted := gofmtOnly(old); formatted == nil || !bytes.Equal(formatted, old) {
		return fixed
	}
	if out := gofmtOnly(fixed); out != nil {
		return out
	}
	return fixed
}

func applyEdits(cs []candidate) ([]*fileState, error) {
	byFile := map[string][]fileEdit{}
	var order []string
	data := map[string][]byte{}
	for _, c := range cs {
		for _, e := range c.edits {
			src, ok := data[e.File]
			if !ok {
				b, err := os.ReadFile(e.File)
				if err != nil {
					return nil, err
				}
				data[e.File], src = b, b
				order = append(order, e.File)
			}
			a, b := offset(src, e.Span.Line, e.Span.Col), offset(src, e.Span.EndLine, e.Span.EndCol)
			if a < 0 || b < a {
				return nil, fmt.Errorf("%s: edit outside the file at %d:%d", e.File, e.Span.Line, e.Span.Col)
			}
			byFile[e.File] = append(byFile[e.File], fileEdit{a, b, e.NewText})
		}
	}
	sort.Strings(order)
	var out []*fileState
	for _, f := range order {
		es := byFile[f]
		sort.SliceStable(es, func(i, j int) bool { return es[i].a < es[j].a })
		var b bytes.Buffer
		src := data[f]
		last := 0
		for _, e := range es {
			if e.a < last {
				continue
			}
			b.Write(src[last:e.a])
			b.WriteString(e.text)
			last = e.b
		}
		b.Write(src[last:])
		out = append(out, &fileState{path: f, old: src, new: reformat(src, b.Bytes())})
	}
	return out, nil
}

// offset converts a 1-based line and byte column to a byte offset, or -1.
func offset(src []byte, line, col int) int {
	if line < 1 {
		return -1
	}
	off := 0
	for l := 1; l < line; l++ {
		i := bytes.IndexByte(src[off:], '\n')
		if i < 0 {
			return -1
		}
		off += i + 1
	}
	off += col - 1
	if off > len(src) {
		return -1
	}
	return off
}

func writeFiles(fs []*fileState, forward bool) {
	for _, f := range fs {
		data := f.new
		if !forward {
			data = f.old
		}
		mode := os.FileMode(0o644)
		if st, err := os.Stat(f.path); err == nil {
			mode = st.Mode().Perm()
		}
		os.WriteFile(f.path, data, mode)
	}
}

// verify type checks the tree after a change (only the changed packages
// and their importers miss the cache) and, with Test, runs the tests of the
// changed directories.
func verify(fo FixOptions, fs []*fileState) (bool, string) {
	errs, err := typeErrors(fo.Options)
	if err != nil {
		return false, err.Error()
	}
	if len(errs) > 0 {
		return false, errs[0]
	}
	if fo.Test {
		dirs := map[string]bool{}
		for _, f := range fs {
			dirs[filepath.Dir(f.path)] = true
		}
		var pkgs []string
		for d := range dirs {
			pkgs = append(pkgs, d)
		}
		sort.Strings(pkgs)
		args := append([]string{"test", "-count=1"}, pkgs...)
		cmd := exec.Command("go", args...)
		cmd.Dir = fo.Dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return false, firstLine(string(out))
		}
	}
	return true, ""
}

// typeErrors type checks the packages (with tests) and returns their
// type and load errors.
func typeErrors(o Options) ([]string, error) {
	o.Only = []string{"vet"}
	r, err := Run(o)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range r.Checks {
		for _, d := range c.Findings {
			if d.Severity == diag.Error {
				out = append(out, fmt.Sprintf("%s:%d: %s", diag.RelPath(r.Dir, d.Span.File), d.Span.Line, d.Message))
			}
		}
	}
	return out, nil
}

func appliedOf(cs []candidate) []Applied {
	var out []Applied
	for _, c := range cs {
		out = append(out, Applied{File: c.d.Span.File, Line: c.d.Span.Line, Check: c.d.Check, Analyzer: c.d.Analyzer, Title: c.d.Fixes[0].Title})
	}
	return out
}

// diffFiles returns unified diffs of the changes (with diff -u).
func diffFiles(fs []*fileState, dir string) string {
	var b strings.Builder
	tmp, err := os.MkdirTemp("", "marmot-fix-diff-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmp)
	for i, f := range fs {
		a, c := filepath.Join(tmp, fmt.Sprintf("%d.old", i)), filepath.Join(tmp, fmt.Sprintf("%d.new", i))
		os.WriteFile(a, f.old, 0o600)
		os.WriteFile(c, f.new, 0o600)
		rel := strings.TrimPrefix(diag.RelPath(dir, f.path), "./")
		out, _ := exec.Command("diff", "-u", "--label", "a/"+rel, "--label", "b/"+rel, a, c).Output()
		b.Write(out)
	}
	return b.String()
}

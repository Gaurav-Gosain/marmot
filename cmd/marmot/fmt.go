package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

type fmtFlags struct {
	list, diff, check *bool
	output            outputFlags
}

func newFmtFlags() (*flag.FlagSet, *fmtFlags) {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	f := &fmtFlags{}
	f.list = fs.Bool("l", false, "only list the files that would change")
	f.diff = fs.Bool("d", false, "print a diff of the changes instead of writing them")
	f.check = fs.Bool("check", false, "change nothing; exit 1 if a file is not formatted")
	f.output = addOutputFlags(fs)
	return fs, f
}

// cmdFmt implements marmot fmt [-l] [-d] [-check] [files, dirs or ./...]:
// gofmt plus goimports. Without -l, -d or -check the files are rewritten in
// place and their names printed, like go fmt.
func cmdFmt(args []string) int {
	fs, ff := newFmtFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("fmt", err)
	}
	rep := ff.output.reporter("fmt")
	targets := fs.Args()
	if len(targets) == 0 {
		targets = []string{"."}
	}
	files, err := goFiles(targets)
	if err != nil {
		return rep.Usage("marmot fmt: " + err.Error())
	}
	src := diag.NewSources()
	write := !*ff.list && !*ff.diff && !*ff.check
	var changedFiles []string
	var ds []diag.Diagnostic
	status := 0
	for _, f := range files {
		in, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "marmot fmt:", err)
			status = 1
			continue
		}
		out, err := src.FormatFile(f, in)
		if err != nil {
			ds = append(ds, diag.FromText(err.Error(), rep.Dir)...)
			status = 1
			continue
		}
		if bytes.Equal(in, out) {
			continue
		}
		changedFiles = append(changedFiles, f)
		rel := diag.RelPath(rep.Dir, f)
		switch {
		case *ff.diff:
			fmt.Print(unifiedDiff(rel, in, out))
		case write:
			st, _ := os.Stat(f)
			if err := os.WriteFile(f, out, st.Mode().Perm()); err != nil {
				fmt.Fprintln(os.Stderr, "marmot fmt:", err)
				status = 1
				continue
			}
		}
		if rep.Mode != diag.ModeJSON && !*ff.diff {
			fmt.Println(rel)
		}
	}
	if len(ds) > 0 {
		rep.Fail(fmtErr(ds))
	}
	if rep.Mode == diag.ModeJSON {
		for _, f := range changedFiles {
			fmt.Fprintf(os.Stderr, "{\"schema\":%d,\"kind\":\"format\",\"file\":%q,\"written\":%v}\n", diag.Schema, f, write)
		}
		diag.WriteResult(os.Stderr, diag.Result{Command: "fmt", OK: status == 0 && !(*ff.check && len(changedFiles) > 0), Errors: len(ds)})
	}
	if *ff.check && len(changedFiles) > 0 {
		return 1
	}
	return status
}

type fmtErr []diag.Diagnostic

func (e fmtErr) Error() string {
	var b strings.Builder
	for _, d := range e {
		fmt.Fprintf(&b, "%s:%d:%d: %s\n", d.Span.File, d.Span.Line, d.Span.Col, d.Message)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// goFiles expands files, directories and dir/... patterns to Go files,
// skipping testdata, vendor and hidden directories.
func goFiles(targets []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if a, err := filepath.Abs(p); err == nil && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, t := range targets {
		recursive := strings.HasSuffix(t, "/...")
		t = strings.TrimSuffix(t, "/...")
		if t == "" {
			t = "."
		}
		st, err := os.Stat(t)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			add(t)
			continue
		}
		filepath.WalkDir(t, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				n := d.Name()
				if p != t && (!recursive || n == "testdata" || n == "vendor" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_")) {
					return filepath.SkipDir
				}
				return nil
			}
			if diag.IsGoFile(d.Name()) {
				add(p)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out, nil
}

// unifiedDiff runs diff -u on the old and new text, or prints both when
// diff is not available.
func unifiedDiff(name string, a, b []byte) string {
	dir, err := os.MkdirTemp("", "marmot-fmt-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	pa, pb := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.WriteFile(pa, a, 0o644)
	os.WriteFile(pb, b, 0o644)
	out, _ := exec.Command("diff", "-u", "--label", name+".orig", "--label", name, pa, pb).Output()
	return string(out)
}

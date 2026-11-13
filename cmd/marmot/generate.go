package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

// marmot generate runs //go:generate directives like go generate, but
// caches what each one writes: the key is the directive, the environment
// it sees, the generator's binary (when it is a file) and the content of
// the package's Go files that the directive did not write itself. When the
// key is in the cache the outputs are restored instead of running the
// command again.

type generateFlags struct {
	force, list, verbose *bool
	run                  *string
	output               outputFlags
}

func newGenerateFlags() (*flag.FlagSet, *generateFlags) {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	g := &generateFlags{}
	g.force = fs.Bool("force", false, "run every directive, ignoring the cache")
	g.list = fs.Bool("n", false, "print the directives and whether they are cached; run nothing")
	g.verbose = fs.Bool("v", false, "print each directive as it runs")
	g.run = fs.String("run", "", "only directives whose text contains this")
	g.output = addOutputFlags(fs)
	return fs, g
}

type genDirective struct {
	file string // absolute path of the file with the directive
	line int
	args string // the command line after //go:generate
}

type genEntry struct {
	Outputs map[string][]byte `json:"outputs"` // path relative to the package dir -> content
}

func cmdGenerate(args []string) int {
	fs, gf := newGenerateFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("generate", err)
	}
	rep := gf.output.reporter("generate")
	targets := fs.Args()
	if len(targets) == 0 {
		targets = []string{"."}
	}
	files, err := goFiles(targets)
	if err != nil {
		return rep.Usage("marmot generate: " + err.Error())
	}
	byDir := map[string][]genDirective{}
	var dirs []string
	for _, f := range files {
		ds, err := scanDirectives(f)
		if err != nil {
			return rep.Usage("marmot generate: " + err.Error())
		}
		if len(ds) > 0 && byDir[filepath.Dir(f)] == nil {
			dirs = append(dirs, filepath.Dir(f))
		}
		for _, d := range ds {
			if *gf.run == "" || strings.Contains(d.args, *gf.run) {
				byDir[filepath.Dir(f)] = append(byDir[filepath.Dir(f)], d)
			}
		}
	}
	sort.Strings(dirs)
	cacheDir := generateCacheDir()
	status, ran, restored := 0, 0, 0
	enc := json.NewEncoder(os.Stderr)
	for _, dir := range dirs {
		for _, d := range byDir[dir] {
			key := directiveKey(d, dir)
			entryPath := filepath.Join(cacheDir, key+".json")
			rel := diag.RelPath(rep.Dir, d.file)
			if *gf.list {
				state := "not cached"
				if _, err := os.Stat(entryPath); err == nil && !*gf.force {
					state = "cached"
				}
				fmt.Printf("%s:%d: %s (%s)\n", rel, d.line, d.args, state)
				continue
			}
			if !*gf.force {
				if e, ok := readGenEntry(entryPath); ok && e.restore(dir) {
					restored++
					if rep.Mode == diag.ModeJSON {
						enc.Encode(map[string]any{"schema": diag.Schema, "kind": "generate", "file": d.file, "line": d.line, "command": d.args, "cached": true})
					} else if *gf.verbose {
						fmt.Fprintf(os.Stderr, "%s:%d: %s (cached)\n", rel, d.line, d.args)
					}
					continue
				}
			}
			if *gf.verbose && rep.Mode != diag.ModeJSON {
				fmt.Fprintf(os.Stderr, "%s:%d: %s\n", rel, d.line, d.args)
			}
			before := snapshot(dir)
			start := time.Now()
			if err := runDirective(d, dir); err != nil {
				fmt.Fprintf(os.Stderr, "%s:%d: %s: %v\n", rel, d.line, d.args, err)
				status = 1
				continue
			}
			ran++
			after := snapshot(dir)
			e := genEntry{Outputs: map[string][]byte{}}
			for p, h := range after {
				if before[p] != h {
					if data, err := os.ReadFile(filepath.Join(dir, p)); err == nil {
						e.Outputs[p] = data
					}
				}
			}
			// A file the last run wrote is still an output when this run
			// wrote the same content.
			for p := range knownOutputs(d, dir) {
				if _, ok := e.Outputs[p]; !ok {
					if data, err := os.ReadFile(filepath.Join(dir, p)); err == nil {
						e.Outputs[p] = data
					}
				}
			}
			// The key is computed from the inputs as they are after the
			// run, minus the outputs, so the next run finds it.
			key = directiveKeyExcluding(d, dir, e.Outputs)
			writeGenEntry(filepath.Join(cacheDir, key+".json"), e)
			if rep.Mode == diag.ModeJSON {
				var outs []string
				for p := range e.Outputs {
					outs = append(outs, p)
				}
				sort.Strings(outs)
				enc.Encode(map[string]any{"schema": diag.Schema, "kind": "generate", "file": d.file, "line": d.line, "command": d.args,
					"cached": false, "outputs": outs, "durationMs": time.Since(start).Milliseconds()})
			}
		}
	}
	if rep.Mode == diag.ModeJSON {
		diag.WriteResult(os.Stderr, diag.Result{Command: "generate", OK: status == 0, DurationMs: time.Since(rep.Start).Milliseconds()})
	} else if !*gf.list && (*gf.verbose || restored > 0) {
		fmt.Fprintf(os.Stderr, "generate: %d ran, %d restored from the cache\n", ran, restored)
	}
	return status
}

// scanDirectives finds //go:generate lines of a file.
func scanDirectives(path string) ([]genDirective, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	var out []genDirective
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		l := sc.Text()
		if rest, ok := strings.CutPrefix(l, "//go:generate "); ok {
			out = append(out, genDirective{file: path, line: n, args: strings.TrimSpace(rest)})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return out, nil
}

// runDirective runs one directive in its package directory with the
// environment go generate sets.
func runDirective(d genDirective, dir string) error {
	words, err := splitGenerate(os.Expand(d.args, func(k string) string { return genEnv(d, dir)[k] }))
	if err != nil || len(words) == 0 {
		return fmt.Errorf("bad directive: %v", err)
	}
	if words[0] == "go" {
		if p, err := exec.LookPath("go"); err == nil {
			words[0] = p
		}
	}
	cmd := exec.Command(words[0], words[1:]...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	env := os.Environ()
	for k, v := range genEnv(d, dir) {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	return cmd.Run()
}

func genEnv(d genDirective, dir string) map[string]string {
	pkg := filepath.Base(dir)
	if f, err := os.ReadFile(d.file); err == nil {
		for l := range strings.SplitSeq(string(f), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "package "); ok {
				pkg = strings.Fields(rest)[0]
				break
			}
		}
	}
	return map[string]string{
		"GOFILE": filepath.Base(d.file), "GOLINE": fmt.Sprint(d.line), "GOPACKAGE": pkg,
		"GOARCH": envOr("GOARCH", runtime.GOARCH), "GOOS": envOr("GOOS", runtime.GOOS), "DOLLAR": "$",
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// splitGenerate splits a directive into words; double quoted strings are
// one word, as go generate does.
func splitGenerate(s string) ([]string, error) {
	var out []string
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		if s[0] == '"' {
			end := 1
			for end < len(s) && (s[end] != '"' || s[end-1] == '\\') {
				end++
			}
			if end == len(s) {
				return nil, fmt.Errorf("unterminated quote")
			}
			out = append(out, strings.ReplaceAll(s[1:end], `\"`, `"`))
			s = s[end+1:]
			continue
		}
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			i = len(s)
		}
		out = append(out, s[:i])
		s = s[i:]
	}
	return out, nil
}

// snapshot hashes every file in dir (not below it).
func snapshot(dir string) map[string]string {
	m := map[string]string{}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil {
			sum := sha256.Sum256(data)
			m[e.Name()] = hex.EncodeToString(sum[:])
		}
	}
	return m
}

func directiveKey(d genDirective, dir string) string {
	// The outputs of a cached run are not inputs: use the entry's outputs
	// when there is one for the plain key's directive.
	return directiveKeyExcluding(d, dir, knownOutputs(d, dir))
}

// knownOutputs returns the outputs recorded for this directive's file and
// line by the last run, so that they are left out of the key.
func knownOutputs(d genDirective, dir string) map[string][]byte {
	p := filepath.Join(generateCacheDir(), "outputs-"+shortHash(d.file+":"+d.args)+".json")
	var names []string
	if data, err := os.ReadFile(p); err == nil {
		json.Unmarshal(data, &names)
	}
	m := map[string][]byte{}
	for _, n := range names {
		m[n] = nil
	}
	return m
}

func directiveKeyExcluding(d genDirective, dir string, outputs map[string][]byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "marmot generate 1\n%s\n%s\n", d.args, filepath.Base(d.file))
	env := genEnv(d, dir)
	keys := make([]string, 0, len(env))
	for k := range env {
		if k != "GOLINE" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, env[k])
	}
	// The generator: the first word, when it is an executable file.
	if words, err := splitGenerate(d.args); err == nil && len(words) > 0 {
		if p, err := exec.LookPath(words[0]); err == nil {
			if data, err := os.ReadFile(p); err == nil {
				sum := sha256.Sum256(data)
				fmt.Fprintf(h, "tool %x\n", sum)
			}
		}
		// go run PKG: the package's sources are inputs too.
		if len(words) > 2 && words[0] == "go" && words[1] == "run" {
			for _, w := range words[2:] {
				if strings.HasPrefix(w, "-") {
					continue
				}
				hashTree(h, filepath.Join(dir, w))
				break
			}
		}
	}
	// The package's files, minus what the directive writes.
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		if !e.IsDir() {
			if _, isOut := outputs[e.Name()]; !isOut {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	for _, n := range names {
		data, _ := os.ReadFile(filepath.Join(dir, n))
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "file %s %x\n", n, sum)
	}
	// Remember which files are outputs for the next key.
	if len(outputs) > 0 {
		var outs []string
		for n := range outputs {
			outs = append(outs, n)
		}
		sort.Strings(outs)
		data, _ := json.Marshal(outs)
		os.MkdirAll(generateCacheDir(), 0o755)
		os.WriteFile(filepath.Join(generateCacheDir(), "outputs-"+shortHash(d.file+":"+d.args)+".json"), data, 0o644)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func hashTree(h io.Writer, root string) {
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			data, _ := os.ReadFile(p)
			sum := sha256.Sum256(data)
			fmt.Fprintf(h, "gen %s %x\n", p, sum)
		}
		return nil
	})
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func generateCacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "marmot", "generate")
	}
	return filepath.Join(os.TempDir(), "marmot-generate")
}

func readGenEntry(p string) (*genEntry, bool) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var e genEntry
	if json.Unmarshal(data, &e) != nil {
		return nil, false
	}
	return &e, true
}

func writeGenEntry(p string, e genEntry) {
	os.MkdirAll(filepath.Dir(p), 0o755)
	data, _ := json.Marshal(e)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, p)
	}
}

// restore writes the outputs back when they differ.
func (e *genEntry) restore(dir string) bool {
	for p, data := range e.Outputs {
		full := filepath.Join(dir, p)
		if old, err := os.ReadFile(full); err == nil && string(old) == string(data) {
			continue
		}
		if os.WriteFile(full, data, 0o644) != nil {
			return false
		}
	}
	return true
}

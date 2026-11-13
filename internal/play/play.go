// Package play is marmot's playground backend: a stateless compile
// service, and a local page on top of it (marmot play).
//
// The service (POST /compile) takes a program (one Go file, or a txtar of
// a few files of package main) and a build mode, and returns the
// compiler's messages, the program at each stage marmot compiles it
// through (x/tools SSA, MLIR with the go dialect, LLVM IR, machine code),
// each output line tied to the source line it comes from, the binary's
// size, and for the wasm target the .wasm itself. It never runs the
// program. Each request works in a temporary directory of its own that
// holds only the request's files; programs may import only the standard
// library (no cgo, no module downloads); compiles run with a CPU limit, a
// wall clock timeout, a cap on concurrent compiles and a short queue.
// The container it is deployed in adds the memory limit and the absence
// of network.
//
// The local page (Options.Local) also offers Run, which builds and runs
// the program on this machine through a Runner.
package play

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/tools/txtar"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

//go:embed static
var static embed.FS

// Limits bound the work of one request.
type Limits struct {
	Timeout    time.Duration // wall clock of each compile (and of a local run)
	CPUSeconds int           // CPU time of each compile (RLIMIT_CPU on Linux)
	MaxOutput  int           // bytes of a stage's text, and of a run's stdout and stderr
	MaxSource  int           // bytes of the request's files together
	MaxFiles   int
	MaxWasm    int   // bytes of a .wasm returned
	Parallel   int   // compiles at a time, server wide
	Queue      int   // requests waiting for a compile slot; more are refused (503)
	MaxBody    int64 // bytes of a request body
}

// DefaultLimits are the service's limits.
var DefaultLimits = Limits{
	Timeout: 30 * time.Second, CPUSeconds: 60, MaxOutput: 1 << 20, MaxSource: 128 << 10, MaxFiles: 16,
	MaxWasm: 16 << 20, Parallel: max(1, min(4, runtime.NumCPU()/2)), Queue: 16, MaxBody: 512 << 10,
}

// Options configure a Server.
type Options struct {
	Marmot  string // the marmot binary that compiles
	Root    string // marmot's root (MARMOT_ROOT of the compiles)
	Limits  Limits
	Local   bool   // serve the page and /api/run (marmot play on a laptop)
	Initial string // the page's first program (Local)
	Runner  Runner // runs programs for /api/run (Local); LocalRunner by default
	// Origins that may call /compile from a browser ("*": any). The
	// Worker in front of the deployed service sets its own CORS headers.
	AllowOrigin string
}

// Server is the compile service, and with Options.Local the page.
type Server struct {
	opt   Options
	slots chan struct{}
	queue chan struct{}
	mu    sync.Mutex
	cache map[string]*Response
	// lastHuman: the human form of the last diagnostics of each request
	// directory.
	lastHuman sync.Map
}

// New returns a server.
func New(opt Options) *Server {
	if opt.Limits.Timeout == 0 {
		opt.Limits = DefaultLimits
	}
	if opt.Runner == nil {
		opt.Runner = LocalRunner{}
	}
	return &Server{opt: opt, slots: make(chan struct{}, opt.Limits.Parallel),
		queue: make(chan struct{}, opt.Limits.Queue+opt.Limits.Parallel), cache: map[string]*Response{}}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /compile", s.handleCompile)
	mux.HandleFunc("OPTIONS /compile", s.handleCompile)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "modes": Modes, "stages": StageNames})
	})
	if s.opt.Local {
		sub, _ := fs.Sub(static, "static")
		mux.Handle("GET /", http.FileServerFS(sub))
		mux.HandleFunc("GET /api/initial", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"source": s.opt.Initial, "modes": Modes, "stages": StageNames, "run": true})
		})
		mux.HandleFunc("POST /api/run", s.handleRun)
	}
	return mux
}

// Request is the body of POST /compile.
type Request struct {
	Source string   `json:"source,omitempty"` // main.go
	Txtar  string   `json:"txtar,omitempty"`  // or a txtar of .go files (and an ignored go.mod)
	Mode   string   `json:"mode"`             // dev, O2 or release
	Stages []string `json:"stages"`           // of StageNames; none: diagnostics only
	Target string   `json:"target,omitempty"` // "" (this machine) or "wasm": also return the .wasm
}

// Stage is one stage's output: its text, and for each text line the line
// of the source it comes from (0: none) in File.
type Stage struct {
	Text string `json:"text"`
	// Lines holds, per line of Text, the line of File (main.go) it comes
	// from, 0 for none.
	Lines []int  `json:"lines"`
	File  string `json:"file,omitempty"`
	// Map is the other direction, for every file of the request: each
	// source line and the lines of Text it became, in file and line
	// order.
	Map   []SourceLines `json:"map"`
	Note  string        `json:"note,omitempty"`
	Error string        `json:"error,omitempty"`
}

// SourceLines is what one source line became in a stage's text.
type SourceLines struct {
	File string `json:"file"`
	Line int    `json:"line"`
	// Ranges are runs of lines of the stage text, 1-based and inclusive:
	// [[3, 5], [9, 9]] is lines 3 to 5 and line 9.
	Ranges [][2]int `json:"ranges"`
}

// finish fills st from the stage text and its per-line source lines:
// the text cut at limit bytes, Lines for main.go, and Map.
func finish(st *Stage, text string, locs []srcLoc, limit int) {
	if len(text) > limit {
		cut := strings.LastIndexByte(text[:limit], '\n')
		if cut < 0 {
			cut = limit
		}
		k := min(strings.Count(text[:cut], "\n")+1, len(locs))
		text = text[:cut] + "\n[output cut at " + strconv.Itoa(limit>>10) + " KB]"
		locs = append(locs[:k:k], srcLoc{})
	}
	st.Text = text
	st.Lines = make([]int, len(locs))
	idx := map[srcLoc]int{}
	st.Map = []SourceLines{}
	for i, l := range locs {
		if l.File == "" || l.Line <= 0 {
			continue
		}
		if l.File == "main.go" {
			st.Lines[i] = l.Line
		}
		j, ok := idx[l]
		if !ok {
			j = len(st.Map)
			idx[l] = j
			st.Map = append(st.Map, SourceLines{File: l.File, Line: l.Line})
		}
		m := &st.Map[j]
		n := i + 1
		if k := len(m.Ranges); k > 0 && m.Ranges[k-1][1] == n-1 {
			m.Ranges[k-1][1] = n
		} else {
			m.Ranges = append(m.Ranges, [2]int{n, n})
		}
	}
	sort.Slice(st.Map, func(a, b int) bool {
		if st.Map[a].File != st.Map[b].File {
			return st.Map[a].File < st.Map[b].File
		}
		return st.Map[a].Line < st.Map[b].Line
	})
}

// Response is the body of the answer to POST /compile.
type Response struct {
	OK          bool             `json:"ok"` // the program builds
	Error       string           `json:"error,omitempty"`
	Diagnostics []Diagnostic     `json:"diagnostics"`
	Human       string           `json:"human,omitempty"` // the messages as marmot prints them: ANSI colours, 72 columns
	Stages      map[string]Stage `json:"stages"`
	Size        *SizeReport      `json:"size,omitempty"`
	Wasm        string           `json:"wasm,omitempty"` // base64
	Millis      int64            `json:"millis"`
}

// Diagnostic is one compiler message.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Col      int    `json:"col,omitempty"`
	EndLine  int    `json:"endLine,omitempty"`
	EndCol   int    `json:"endCol,omitempty"`
	Rendered string `json:"rendered,omitempty"`
}

// Modes are the build modes.
var Modes = []string{"dev", "O2", "release"}

var modeFlags = map[string][]string{"dev": nil, "O2": {"-O", "2"}, "release": {"-release"}}

// StageNames are the stages a request can ask for.
var StageNames = []string{"ssa", "mlir", "llvm", "asm", "size"}

// errBusy is the answer when the queue is full.
var errBusy = errors.New("the compile service is busy; try again in a moment")

type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
}

func (s *Server) cors(w http.ResponseWriter) {
	if o := s.opt.AllowOrigin; o != "" {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
}

func (s *Server) handleCompile(w http.ResponseWriter, r *http.Request) {
	s.cors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var q Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.opt.Limits.MaxBody)).Decode(&q); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request: " + err.Error()})
		return
	}
	res, err := s.Compile(r.Context(), &q)
	if err != nil {
		code := http.StatusBadRequest
		var he *httpError
		if errors.As(err, &he) {
			code = he.code
		}
		if errors.Is(err, errBusy) {
			code = http.StatusServiceUnavailable
			w.Header().Set("Retry-After", "5")
		}
		writeJSON(w, code, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// files returns the request's files by name, checked.
func (s *Server) files(q *Request) (map[string][]byte, error) {
	files := map[string][]byte{}
	switch {
	case q.Txtar != "" && q.Source != "":
		return nil, errors.New("send source or txtar, not both")
	case q.Txtar != "":
		for _, f := range txtar.Parse([]byte(q.Txtar)).Files {
			files[f.Name] = f.Data
		}
	default:
		files["main.go"] = []byte(q.Source)
	}
	total := 0
	for name, data := range files {
		if name == "go.mod" || name == "go.sum" {
			delete(files, name) // the service writes its own: standard library only
			continue
		}
		if !reFileName.MatchString(name) {
			return nil, fmt.Errorf("file %q: names are plain .go file names (no directories)", name)
		}
		total += len(data)
	}
	if len(files) == 0 {
		return nil, errors.New("no Go files")
	}
	if len(files) > s.opt.Limits.MaxFiles {
		return nil, fmt.Errorf("%d files: at most %d", len(files), s.opt.Limits.MaxFiles)
	}
	if total > s.opt.Limits.MaxSource {
		return nil, fmt.Errorf("%d bytes of source: at most %d", total, s.opt.Limits.MaxSource)
	}
	return files, nil
}

var reFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*\.go$`)

// checkImports turns imports the service cannot build into diagnostics:
// cgo, and anything outside the standard library.
func checkImports(files map[string][]byte) []Diagnostic {
	var out []Diagnostic
	fset := token.NewFileSet()
	for name, data := range files {
		f, err := parser.ParseFile(fset, name, data, parser.ImportsOnly)
		if err != nil {
			continue // the compiler reports it
		}
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			p := fset.Position(im.Path.Pos())
			msg := ""
			switch {
			case path == "C":
				msg = "cgo is not available in the playground"
			case strings.Contains(strings.Split(path, "/")[0], "."):
				msg = fmt.Sprintf("the playground builds with the standard library only; %s is a module", path)
			}
			if msg != "" {
				out = append(out, Diagnostic{Severity: "error", Message: msg, File: name, Line: p.Line, Col: p.Column,
					EndLine: p.Line, EndCol: p.Column + len(im.Path.Value)})
			}
		}
	}
	return out
}

// Compile answers a request.
func (s *Server) Compile(ctx context.Context, q *Request) (*Response, error) {
	if _, ok := modeFlags[q.Mode]; !ok {
		if q.Mode != "" {
			return nil, fmt.Errorf("mode %q: one of dev, O2, release", q.Mode)
		}
		q.Mode = "dev"
	}
	for _, st := range q.Stages {
		if !contains(StageNames, st) {
			return nil, fmt.Errorf("stage %q: one of %s", st, strings.Join(StageNames, ", "))
		}
	}
	if q.Target != "" && q.Target != "wasm" {
		return nil, fmt.Errorf("target %q: \"\" or wasm", q.Target)
	}
	files, err := s.files(q)
	if err != nil {
		return nil, err
	}
	key := cacheKey(files, q)
	s.mu.Lock()
	if r, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return r, nil
	}
	s.mu.Unlock()
	t0 := time.Now()
	res := &Response{Stages: map[string]Stage{}, Diagnostics: []Diagnostic{}}
	if bad := checkImports(files); len(bad) > 0 {
		res.Diagnostics = bad
		var b strings.Builder
		for _, d := range bad {
			fmt.Fprintf(&b, "\x1b[1;31merror\x1b[0m: %s\n --> %s:%d:%d\n", d.Message, d.File, d.Line, d.Col)
		}
		res.Human = b.String()
		res.Millis = time.Since(t0).Milliseconds()
		return res, nil
	}
	select {
	case s.queue <- struct{}{}:
		defer func() { <-s.queue }()
	default:
		return nil, errBusy
	}
	dir, err := workspace(files)
	defer func() { _ = os.RemoveAll(dir) }() // the request's temporary directory
	if err != nil {
		return nil, err
	}
	// The executable first: its diagnostics are the program's, and the
	// stages need a program that builds.
	exeFlags := []string{"-o", "prog"}
	if q.Target == "wasm" {
		exeFlags = []string{"-target", "wasip1/wasm", "-o", "prog.wasm"}
	}
	diags, other, ok := s.build(ctx, dir, q.Mode, exeFlags...)
	res.Diagnostics, res.OK = diags, ok
	if h, ok := s.lastHuman.LoadAndDelete(dir); ok {
		res.Human = h.(string)
	}
	if !ok {
		res.Error = other
		res.Millis = time.Since(t0).Milliseconds()
		return res, nil
	}
	if q.Target == "wasm" {
		if data, err := os.ReadFile(filepath.Join(dir, "prog.wasm")); err == nil && len(data) <= s.opt.Limits.MaxWasm {
			res.Wasm = base64.StdEncoding.EncodeToString(data)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, st := range q.Stages {
		if st == "size" {
			exe := filepath.Join(dir, "prog")
			if q.Target == "wasm" {
				exe += ".wasm"
			}
			if rep, err := sizeReport(exe); err == nil {
				res.Size = rep
			}
			continue
		}
		wg.Go(func() {
			r := s.stage(ctx, dir, q.Mode, st)
			mu.Lock()
			res.Stages[st] = r
			mu.Unlock()
		})
	}
	wg.Wait()
	res.Millis = time.Since(t0).Milliseconds()
	s.mu.Lock()
	if len(s.cache) >= 128 {
		s.cache = map[string]*Response{}
	}
	s.cache[key] = res
	s.mu.Unlock()
	return res, nil
}

func cacheKey(files map[string][]byte, q *Request) string {
	h := sha256.New()
	for _, n := range sortedNames(files) {
		fmt.Fprintf(h, "%s %d\n", n, len(files[n]))
		h.Write(files[n])
	}
	fmt.Fprintf(h, "%s %s %q", q.Mode, q.Target, q.Stages)
	return hex.EncodeToString(h.Sum(nil))
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// workspace writes the files as package main of a module in a new
// temporary directory.
func workspace(files map[string][]byte) (string, error) {
	dir, err := os.MkdirTemp("", "marmot-play-")
	if err != nil {
		return "", err
	}
	// The compiler records resolved paths (/private/var on macOS).
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module play\n\ngo 1.24\n"), 0o644); err != nil {
		return dir, err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return dir, err
		}
	}
	return dir, nil
}

// build runs marmot build in dir with the mode's flags and extra flags,
// waiting for a compile slot, and returns the diagnostics, other output,
// and whether it worked.
func (s *Server) build(ctx context.Context, dir, mode string, extra ...string) ([]Diagnostic, string, bool) {
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, "canceled", false
	}
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(ctx, s.opt.Limits.Timeout)
	defer cancel()
	args := append(append([]string{"build"}, modeFlags[mode]...), extra...)
	args = append(args, ".")
	var cmd *exec.Cmd
	if runtime.GOOS == "linux" && s.opt.Limits.CPUSeconds > 0 {
		// RLIMIT_CPU for the compile and its tools.
		cmd = exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", `ulimit -t "$0"; exec "$@"`, strconv.Itoa(s.opt.Limits.CPUSeconds), s.opt.Marmot}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, s.opt.Marmot, args...)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MARMOT_OUTPUT=json", "MARMOT_ROOT="+s.opt.Root,
		"GOPROXY=off", "GOFLAGS=-mod=mod", "GONOSUMDB=*", "GOTOOLCHAIN=local", "CGO_ENABLED=0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	out, err := cmd.CombinedOutput()
	diags, other := parseDiagnostics(out)
	if len(diags) > 0 {
		s.lastHuman.Store(dir, humanText(out, dir))
	}
	if ctx.Err() != nil {
		other = strings.TrimSpace(other + "\nthe compile took too long")
	}
	if err != nil && other == "" && len(diags) == 0 {
		other = err.Error()
	}
	return diags, other, err == nil
}

// parseDiagnostics reads marmot's JSON output.
func parseDiagnostics(out []byte) ([]Diagnostic, string) {
	diags := []Diagnostic{}
	var other []string
	for _, l := range strings.Split(string(out), "\n") {
		var d struct {
			Kind     string `json:"kind"`
			Severity string `json:"severity"`
			Code     string `json:"code"`
			Message  string `json:"message"`
			Rendered string `json:"rendered"`
			Span     struct {
				File                       string
				Line, Col, EndLine, EndCol int
			} `json:"span"`
		}
		if json.Unmarshal([]byte(l), &d) != nil {
			if strings.TrimSpace(l) != "" {
				other = append(other, l)
			}
			continue
		}
		if d.Kind != "diagnostic" {
			continue
		}
		dg := Diagnostic{Severity: d.Severity, Code: d.Code, Message: d.Message, Rendered: d.Rendered}
		if d.Span.File != "" {
			// Only the request's files are named; other paths (the
			// server's) are not shown.
			dg.File = filepath.Base(d.Span.File)
			dg.Line, dg.Col, dg.EndLine, dg.EndCol = d.Span.Line, d.Span.Col, d.Span.EndLine, d.Span.EndCol
			dg.Rendered = strings.ReplaceAll(dg.Rendered, filepath.Dir(d.Span.File)+string(filepath.Separator), "")
		}
		diags = append(diags, dg)
	}
	return diags, strings.Join(other, "\n")
}

// humanText renders marmot's JSON diagnostics as it prints them for a
// person, in colour and 72 columns wide, with paths relative to dir.
func humanText(out []byte, dir string) string {
	var ds []diag.Diagnostic
	for _, l := range strings.Split(string(out), "\n") {
		var d diag.Diagnostic
		if json.Unmarshal([]byte(l), &d) == nil && d.Kind == "diagnostic" {
			ds = append(ds, d)
		}
	}
	if len(ds) == 0 {
		return ""
	}
	var b bytes.Buffer
	h := &diag.Human{W: &b, Style: diag.Colors, Dir: dir, Width: 72}
	h.Render(ds)
	return strings.ReplaceAll(b.String(), dir+string(filepath.Separator), "")
}

// stage builds one stage in dir (where the program already builds).
func (s *Server) stage(ctx context.Context, dir, mode, stage string) Stage {
	files := requestFiles(dir)
	limit := s.opt.Limits.MaxOutput
	emit := func(flags ...string) string {
		if _, other, ok := s.build(ctx, dir, mode, flags...); !ok {
			return firstNonEmpty(other, "the stage did not build")
		}
		return ""
	}
	var st Stage
	switch stage {
	case "ssa":
		text, lines, err := ssaText(s.opt.Root, dir)
		if err != nil {
			return Stage{Error: err.Error()}
		}
		st = Stage{Note: "x/tools SSA of package main, as marmot's frontend builds it"}
		finish(&st, text, lines, limit)
	case "mlir":
		if e := emit("-dialect", "go", "-emit", "mlir", "-o", "out.mlir"); e != "" {
			return Stage{Error: e}
		}
		data, _ := os.ReadFile(filepath.Join(dir, "out.mlir"))
		text, lines := mlirLines(string(data), files)
		st = Stage{Note: "package main in MLIR with the go dialect (go.* operations among the LLVM dialect's), as the frontend emits it"}
		finish(&st, text, lines, limit)
	case "llvm":
		if e := emit("-emit", "llvm", "-o", "out.ll"); e != "" {
			return Stage{Error: e}
		}
		data, _ := os.ReadFile(filepath.Join(dir, "out.ll"))
		text, lines := llvmLines(string(data), files)
		st = Stage{Note: "package main in LLVM IR after the backend's pipeline for the mode"}
		finish(&st, text, lines, limit)
	case "asm":
		st = Stage{Note: "package main's " + runtime.GOARCH + " machine code"}
		// Dev objects (tier0) carry no line table: disassemble the
		// executable the build already linked, with the lines of
		// marmot's pc table, as tracebacks read them.
		if mode == "dev" {
			if text, lines, err := asmFromExe(filepath.Join(dir, "prog"), files); err == nil {
				st.Note += ", from the linked executable with the lines of marmot's pc table"
				finish(&st, text, lines, limit)
				break
			}
		}
		if e := emit("-emit", "obj", "-o", "out.o"); e != "" {
			return Stage{Error: e}
		}
		text, lines, err := asmLines(filepath.Join(dir, "out.o"), files)
		if err != nil {
			return Stage{Error: err.Error()}
		}
		finish(&st, text, lines, limit)
	}
	st.File = "main.go"
	// No server paths in what goes out.
	st.Text = strings.NewReplacer(dir+string(filepath.Separator), "", dir, ".", s.opt.Root, "$MARMOT").Replace(st.Text)
	return st
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// ---- local mode: Run ----

// RunResult is what running a program gave.
type RunResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Exit     int    `json:"exit"`
	TimedOut bool   `json:"timedOut"`
	Millis   int64  `json:"millis"`
}

// Runner runs a built program in dir.
type Runner interface {
	Run(ctx context.Context, dir, exe string, lim Limits) RunResult
}

// LocalRunner runs programs as child processes: a timeout, an output cap,
// a process group killed whole, dir as the working directory and a small
// environment. It does not confine the program: local use only.
type LocalRunner struct{}

// Run implements Runner.
func (LocalRunner) Run(ctx context.Context, dir, exe string, lim Limits) RunResult {
	ctx, cancel := context.WithTimeout(ctx, lim.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + dir, "TMPDIR=" + dir, "PATH=/usr/bin:/bin"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	out, errb := &capped{max: lim.MaxOutput}, &capped{max: lim.MaxOutput}
	cmd.Stdout, cmd.Stderr = out, errb
	t0 := time.Now()
	err := cmd.Run()
	r := RunResult{Stdout: out.String(), Stderr: errb.String(), Millis: time.Since(t0).Milliseconds()}
	var ee *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		r.TimedOut, r.Exit = true, -1
	case errors.As(err, &ee):
		r.Exit = ee.ExitCode()
	case err != nil:
		r.Stderr += err.Error()
		r.Exit = -1
	}
	return r
}

type capped struct {
	bytes.Buffer
	max  int
	lost bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.Len(); room < len(p) {
		if room > 0 {
			c.Buffer.Write(p[:room])
		}
		c.lost = true
		return len(p), nil
	}
	return c.Buffer.Write(p)
}

func (c *capped) String() string {
	if c.lost {
		return c.Buffer.String() + "\n[output cut]"
	}
	return c.Buffer.String()
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var q Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.opt.Limits.MaxBody)).Decode(&q); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if _, ok := modeFlags[q.Mode]; !ok {
		q.Mode = "dev"
	}
	files, err := s.files(&q)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	dir, err := workspace(files)
	defer func() { _ = os.RemoveAll(dir) }() // the request's temporary directory
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	t0 := time.Now()
	diags, other, ok := s.build(r.Context(), dir, q.Mode, "-o", "prog")
	res := map[string]any{"diagnostics": diags, "buildMillis": time.Since(t0).Milliseconds()}
	if ok {
		lim := s.opt.Limits
		lim.Timeout = 10 * time.Second
		res["run"] = s.opt.Runner.Run(r.Context(), dir, filepath.Join(dir, "prog"), lim)
		if st, err := os.Stat(filepath.Join(dir, "prog")); err == nil {
			res["size"] = st.Size()
		}
	} else {
		res["error"] = other
	}
	writeJSON(w, http.StatusOK, res)
}

func sortedNames(m map[string][]byte) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Package driver runs the marmot pipeline: load, SSA, MLIR codegen per
// package, backend compilation to objects (cached), and linking.
package driver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/codegen"
	"github.com/Gaurav-Gosain/marmot/internal/loader"
	"github.com/Gaurav-Gosain/marmot/internal/specialize"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// Config holds the build options from the command line.
type Config struct {
	Root     string // repository root
	Dir      string // working directory
	Output   string // -o
	OptLevel string // -O
	Emit     string // mlir, llvm, obj, exe
	Work     bool   // keep the work directory
	Verbose  bool
	PrintCmd bool // -x
	NoCache  bool
	GC       string
	Target   string
	Dialect  string
	Release  bool   // whole program optimization (-wpo) and all specializations
	Test     bool   // build the package's test binary (go list -test main)
	PGOGen   bool   // instrument for profile generation
	PGO      string // indexed profile to optimize with
	Strip    bool   // -s: no pc table
	Trace    string // -trace=full: inlined frames in the pc table of release builds
	Tags     []string
	Patterns []string
	Stderr   io.Writer
}

// FindRoot locates the marmot repository root.
func FindRoot() (string, error) {
	if r := os.Getenv("MARMOT_ROOT"); r != "" {
		return r, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	root := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // build/bin/marmot
	if _, err := os.Stat(filepath.Join(root, "lib", "src")); err == nil {
		return root, nil
	}
	return "", fmt.Errorf("cannot find the marmot root; set MARMOT_ROOT")
}

const llvmBin = "/opt/homebrew/opt/llvm@22/bin"

func llvmTool(name string) string {
	if d := os.Getenv("LLVM_BIN"); d != "" {
		return filepath.Join(d, name)
	}
	return filepath.Join(llvmBin, name)
}

type build struct {
	cfg        Config
	keepLocals bool           // link without -x (the first link of a pc table build)
	lastOutput string         // combined output of the last tool run (run)
	modKeys    []string       // per module: a key of its text, or ""
	stream     *backendStream // backend compiling modules as they are written
	work       string
	start      time.Time
	goos       string // target
	goarch     string
	testDir    string   // directory of the package under test
	dylibs     []string // from //go:cgo_import_dynamic
	out        string
	incr       bool // packages without a cached module are loaded from source, the others from export data
	noIncr     bool
	keyMu      sync.Mutex
	keyMemo    map[string]string // srcKey by package path
	keyBase    string
	progFlags  string // incremental builds: program flags the module keys assume
	partMu     sync.Mutex
	parts      []codegen.Module // modules after the first of packages split in parts
	partKeys   []string
}

func (b *build) haveBackend() bool {
	return fileExists(filepath.Join(b.cfg.Root, "build", "bin", "marmot-backend")) && os.Getenv("MARMOT_NO_BACKEND") == ""
}

func (b *build) logf(format string, args ...any) {
	if b.cfg.Verbose {
		fmt.Fprintf(b.cfg.Stderr, "marmot: %6.0fms "+format+"\n", append([]any{float64(time.Since(b.start).Microseconds()) / 1000}, args...)...)
	}
}

// BuildTest builds the test binary of one package and returns it with the
// directory the tests must run in.
func BuildTest(cfg Config) (exe, dir string, err error) {
	cfg.Test = true
	b, err := build1(cfg)
	if b != nil {
		dir = b.testDir
	}
	return b.out, dir, err
}

// Build compiles the packages and writes the output. It returns the output
// path.
func Build(cfg Config) (string, error) {
	b, err := build1(cfg)
	if err != nil {
		return "", err
	}
	return b.out, nil
}

func build1(cfg Config) (*build, error) {
	b := &build{cfg: cfg}
	out, err := b.run1()
	if errors.Is(err, errFlagsChanged) {
		b.logf("%v; building again", err)
		b = &build{cfg: cfg}
		out, err = b.run1()
	}
	if errors.Is(err, errIncrementalMiss) {
		b.logf("%v; loading every package from source", err)
		b = &build{cfg: cfg, noIncr: true}
		out, err = b.run1()
	}
	b.out = out
	return b, err
}

func (b *build) run1() (string, error) {
	cfg := b.cfg
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
		b.cfg.Stderr = os.Stderr
	}
	b.start = time.Now()
	work, err := os.MkdirTemp("", "marmot-")
	if err != nil {
		return "", err
	}
	b.work = work
	markWorkDir(work, cfg.Work)
	sweepWorkDirs(b.logf)
	defer func() {
		if before, after, err := TrimCache(cfg.Root, false); err == nil && before != after {
			b.logf("cache trimmed: %s -> %s", fmtMB(before), fmtMB(after))
		}
	}()
	if cfg.Work {
		fmt.Fprintf(cfg.Stderr, "WORK=%s\n", work)
	} else {
		defer os.RemoveAll(work)
		cleanupDirs.add(work)
		defer cleanupDirs.remove(work)
	}
	stop := handleSignals()
	defer stop()

	goos, goarch, err := parseTarget(cfg.Target)
	if err != nil {
		return "", err
	}
	b.goos, b.goarch = goos, goarch
	stamp := ""
	if cfg.Output != "" && cfg.Emit == "exe" && !cfg.Test {
		stamp = b.buildStamp(goos, goarch)
		if b.upToDate(stamp, cfg.Output) {
			b.logf("up to date (stamp %s)", stamp)
			return cfg.Output, nil
		}
	}
	b.logf("lifetime transforms on: %s", codegen.LifetimeSummary())
	if err := acquireSlot(b.logf); err != nil {
		return "", err
	}
	defer releaseSlot()
	lcfg := loader.Config{
		Dir: cfg.Dir, Root: cfg.Root, Tags: cfg.Tags, GOOS: goos, GOARCH: goarch, Patterns: cfg.Patterns,
		Log: b.logf, Tests: cfg.Test,
		BeforeSSA: func(all []*packages.Package) error {
			return specialize.BeforeSSA(&specialize.Input{Packages: all, Release: cfg.Release, GOOS: goos, GOARCH: goarch, Log: b.logf})
		},
		AfterSSA: func(prog *ssa.Program, pkgs []*ssa.Package) error {
			return specialize.AfterSSA(&specialize.Input{Prog: prog, Pkgs: pkgs, Release: cfg.Release, GOOS: goos, GOARCH: goarch, Log: b.logf})
		},
	}
	if b.incr = b.incremental(); b.incr {
		b.incrementalConfig(&lcfg)
	}
	lp, err := loader.Load(lcfg)
	if err != nil {
		return "", err
	}
	b.logf("loaded %d packages", len(lp.All))
	if len(lp.Packages) != 1 {
		return "", fmt.Errorf("build of %d packages is not supported; name one main package", len(lp.Packages))
	}
	root := lp.Packages[0]
	if cfg.Test {
		// Tests run in the directory of the package under test.
		for _, p := range lp.All {
			if strings.HasSuffix(root.ID, ".test") && p.PkgPath == strings.TrimSuffix(root.PkgPath, ".test") && len(p.GoFiles) > 0 {
				b.testDir = filepath.Dir(p.GoFiles[0])
			}
		}
	}
	if root.Name != "main" && cfg.Emit == "exe" {
		return "", fmt.Errorf("%s is not a main package", root.PkgPath)
	}

	var keepExported, reflectCall bool
	if b.incr {
		if keepExported, reflectCall, err = b.checkProgFlags(lp); err != nil {
			return "", err
		}
	}
	files := map[*types.Package][]*ast.File{}
	var ssaPkgs []*ssa.Package
	for _, p := range lp.All {
		files[p.Types] = p.Syntax
		if sp := lp.SSAPkgs[p.Types]; sp != nil {
			ssaPkgs = append(ssaPkgs, sp)
		}
	}
	cg, err := codegen.NewProgram(lp.SSA, ssaPkgs, files, codegen.Options{GOOS: goos, GOARCH: goarch, Sizes: lp.Sizes, GoDialect: cfg.Dialect == "go", Release: cfg.Release, MergedGOROOT: lp.GOROOT, XDefs: b.xdefs(),
		Separate: b.incr, Facts: incrementalFacts(lp), KeepExported: keepExported, ReflectCall: reflectCall})
	if err != nil {
		return "", err
	}
	infos := map[*types.Package]*types.Info{}
	for _, p := range lp.All {
		infos[p.Types] = p.TypesInfo
	}
	cg.SetTypesInfo(infos)
	mainSSA := lp.SSAPkgs[root.Types]

	// Generate MLIR for every package in parallel, skipping packages whose
	// inputs are unchanged since a previous build produced their object.
	pkgs := cg.Packages()
	n := len(pkgs)
	if root.Name == "main" {
		n++
	}
	mods := make([]codegen.Module, n)
	pkgKeys := b.packageKeys(lp, cg, pkgs)
	if b.canStream() {
		st, err := b.startStream(n)
		if err != nil {
			return "", err
		}
		b.stream = st
		defer func() {
			if b.stream != nil {
				b.stream.abort()
			}
		}()
	}
	cacheable := func(i int) bool {
		return i < len(pkgs) && pkgKeys[i] != "" && !cfg.NoCache && cfg.Emit != "mlir"
	}
	gen := func(need func(i int) bool) error {
		errs := make([]error, n)
		var wg sync.WaitGroup
		sem := make(chan struct{}, genJobs())
		for i := 0; i < n; i++ {
			if !need(i) || mods[i].Text != "" || mods[i].Path != "" {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				var parts []codegen.Module
				switch {
				case i == len(pkgs):
					mods[i], errs[i] = cg.GenerateMain(mainSSA)
				case b.partInstrs() > 0:
					parts, errs[i] = cg.GenerateParts(pkgs[i], b.partInstrs())
					if errs[i] == nil {
						mods[i], parts = parts[0], parts[1:]
					}
				default:
					mods[i], errs[i] = cg.Generate(pkgs[i])
				}
				if errs[i] == nil {
					errs[i] = b.spillParts(i, &mods[i], parts, cacheable(i), pkgKeys, n)
				}
			}()
		}
		wg.Wait()
		var msgs []string
		for _, e := range errs {
			if e != nil {
				msgs = append(msgs, e.Error())
			}
		}
		if len(msgs) > 0 {
			return fmt.Errorf("%s", strings.Join(msgs, "\n"))
		}
		return nil
	}
	// Modules without a package key (file arguments, the entry module) are
	// always generated; they are small.
	if err := gen(func(i int) bool { return !cacheable(i) }); err != nil {
		return "", err
	}
	skipped := 0
	for i := 0; i < len(pkgs); i++ {
		if cacheable(i) {
			if paths, ok := b.cachedParts(pkgKeys[i]); ok {
				mods[i] = codegen.Module{Name: pkgs[i].Pkg.Path(), Path: paths[0]}
				if b.stream != nil {
					b.stream.send(i, paths[0], pkgKeys[i]+"-"+moduleTextVersion)
				}
				for c, path := range paths[1:] {
					b.addPart(codegen.Module{Name: pkgs[i].Pkg.Path(), Path: path}, partKey(pkgKeys[i], c+1)+"-"+moduleTextVersion, n)
				}
				skipped++
			}
		}
	}
	b.logf("codegen skipped for %d unchanged packages", skipped)
	if err := b.checkIncremental(lp, pkgs, mods); err != nil {
		return "", err
	}
	if err := gen(func(i int) bool { return mods[i].Path == "" }); err != nil {
		return "", err
	}
	// In parallel: the type aliasing and the write take about 1 s on a large
	// program's 350 MB of text.
	eachParallel(len(pkgs), func(i int) {
		if cacheable(i) && mods[i].Path == "" {
			mods[i].Path = b.saveModule(pkgKeys[i], mods[i].Text)
		}
	})
	// The package key stands for a module's text (the MLIR cache relies on
	// that too), so the backend need not read and hash unchanged modules.
	b.modKeys = make([]string, len(mods))
	for i := 0; i < len(pkgs); i++ {
		if cacheable(i) && mods[i].Path != "" {
			b.modKeys[i] = pkgKeys[i] + "-" + moduleTextVersion
		}
	}
	mods = append(mods, b.parts...)
	b.modKeys = append(b.modKeys, b.partKeys...)
	b.logf("generated %d modules", len(mods))

	out := cfg.Output
	if out == "" {
		out = defaultOutput(root, cfg.Emit)
	}

	if cfg.Emit == "mlir" {
		// The root package's module.
		for i, p := range pkgs {
			if p == mainSSA {
				return out, os.WriteFile(out, []byte(mods[i].Text), 0o644)
			}
		}
	}

	var objs []string
	if b.stream != nil {
		st := b.stream
		b.stream = nil
		objs, err = st.finish(b)
	} else {
		objs, err = b.compile(mods, cfg.Emit)
	}
	if err != nil {
		return "", err
	}
	if cfg.Emit == "exe" {
		objs = expandParts(objs)
	}
	b.logf("compiled %d objects", len(objs))
	switch cfg.Emit {
	case "llvm", "obj":
		for i, p := range pkgs {
			if p == mainSSA {
				return out, copyFile(objs[i], out)
			}
		}
	}
	b.dylibs = cg.DynamicLibraries()
	if err := b.link(objs, out); err != nil {
		return "", err
	}
	b.logf("linked %s", out)
	b.saveStamp(stamp, out)
	return out, nil
}

// spill writes module i to its file as soon as it is generated (the MLIR
// cache for a cacheable module, else the work directory) and drops its
// text, so the texts of all modules are never held at once.
func (b *build) spill(i int, m *codegen.Module, cacheable bool, keys []string) error {
	if b.cfg.Emit == "mlir" {
		return nil
	}
	if cacheable {
		if p := b.saveModule(keys[i], m.Text); p != "" {
			m.Path, m.Text = p, ""
			if b.stream != nil {
				b.stream.send(i, p, keys[i]+"-"+moduleTextVersion)
			}
			return nil
		}
	}
	p := filepath.Join(b.work, fmt.Sprintf("m%03d.mlir", i))
	text := aliasTypes(m.Text)
	if b.cfg.Work {
		text = "// " + m.Name + "\n" + text
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		return err
	}
	m.Path, m.Text = p, ""
	if b.stream != nil {
		b.stream.send(i, p, "")
	}
	return nil
}

func defaultOutput(p *packages.Package, emit string) string {
	name := filepath.Base(p.PkgPath)
	if p.PkgPath == "command-line-arguments" && len(p.GoFiles) > 0 {
		name = strings.TrimSuffix(filepath.Base(p.GoFiles[0]), ".go")
	}
	switch emit {
	case "mlir":
		return name + ".mlir"
	case "llvm":
		return name + ".ll"
	case "obj":
		return name + ".o"
	}
	return name
}

// compile turns modules into object files (or .ll for -emit llvm), reusing
// cached objects keyed by the MLIR text and the flags.
func (b *build) compile(mods []codegen.Module, emit string) ([]string, error) {
	backend := filepath.Join(b.cfg.Root, "build", "bin", "marmot-backend")
	haveBackend := b.haveBackend()
	if b.cfg.Dialect == "go" && !haveBackend {
		return nil, fmt.Errorf("-dialect=go needs build/bin/marmot-backend")
	}
	ext := ".o"
	if emit == "llvm" {
		ext = ".ll"
	}
	objs := make([]string, len(mods))
	ins := make([]string, len(mods))
	werrs := make([]error, len(mods))
	eachParallel(len(mods), func(i int) {
		m := mods[i]
		ins[i] = m.Path
		if m.Text != "" {
			ins[i] = filepath.Join(b.work, fmt.Sprintf("m%03d.mlir", i))
			text := aliasTypes(m.Text)
			if b.cfg.Work {
				text = "// " + m.Name + "\n" + text
			}
			werrs[i] = os.WriteFile(ins[i], []byte(text), 0o644)
		}
		objs[i] = filepath.Join(b.work, fmt.Sprintf("m%03d%s", i, ext))
	})
	for _, err := range werrs {
		if err != nil {
			return nil, err
		}
	}
	if haveBackend {
		// The backend caches every step ("Batch driver and
		// cache"); it needs all modules for cross-package summaries.
		var list strings.Builder
		for i := range mods {
			if i < len(b.modKeys) && b.modKeys[i] != "" {
				fmt.Fprintf(&list, "%s %s %s\n", ins[i], objs[i], b.modKeys[i])
			} else {
				fmt.Fprintf(&list, "%s %s\n", ins[i], objs[i])
			}
		}
		listFile := filepath.Join(b.work, "jobs.txt")
		os.WriteFile(listFile, []byte(list.String()), 0o644)
		emitKind := "obj"
		if emit == "llvm" {
			emitKind = "llvm"
		}
		if err := b.run(backend, b.backendArgs(emitKind, listFile)...); err != nil {
			return nil, err
		}
		b.logBackendTimes()
		return objs, nil
	}
	// Fallback without the backend: mlir-translate and clang per module.
	var wg sync.WaitGroup
	errs := make([]error, len(mods))
	sem := make(chan struct{}, maxJobs())
	for i := range mods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ll := strings.TrimSuffix(objs[i], ext) + ".ll"
			if emit == "llvm" {
				ll = objs[i]
			}
			if err := b.run(llvmTool("mlir-translate"), "--mlir-to-llvmir", ins[i], "-o", ll); err != nil {
				errs[i] = err
				return
			}
			if emit != "llvm" {
				errs[i] = b.run(llvmTool("clang"), "-c", "-O"+b.cfg.OptLevel, "-Wno-override-module", "-o", objs[i], "-x", "ir", ll)
			}
		}()
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	return objs, nil
}

func (b *build) link(objs []string, out string) error {
	rt := b.rtLib("librtcore.a")
	if !fileExists(rt) {
		return fmt.Errorf("%s is missing; run make rtcore", rt)
	}
	if b.cross() {
		return b.linkBundle(objs, rt, out)
	}
	if b.wantPcln() {
		return b.linkWithPcln(objs, func(extra []string, o string) error {
			return b.link1(objs, rt, o, extra)
		}, out)
	}
	return b.link1(objs, rt, out, nil)
}

// link1 runs the linker once.
func (b *build) link1(objs []string, rt, out string, extra []string) error {
	args := []string{"-o", out}
	args = append(args, objs...)
	args = append(args, rt, "-lpthread")
	if b.cfg.PGOGen {
		args = append(args, "-fprofile-generate")
	}
	args = append(args, linkLibraries(b.dylibs)...)
	if mp := os.Getenv("MARMOT_LINKMAP"); mp != "" && runtime.GOOS == "darwin" {
		args = append(args, "-Wl,-map,"+mp)
	}
	if runtime.GOOS == "darwin" {
		// Only main is exported: every other symbol becomes local and -x
		// drops it from the symbol table (Go symbol names are long).
		args = append(args, "-Wl,-dead_strip")
		if os.Getenv("MARMOT_KEEP_SYMBOLS") == "" {
			if !b.keepLocals {
				args = append(args, "-Wl,-x")
			}
			args = append(args, "-Wl,-exported_symbol,_main")
			if !b.wantPcln() {
				args = append(args, "-Wl,-S") // no debug map
			}
		}
	} else {
		args = append(args, "-Wl,--gc-sections")
		if !b.wantPcln() {
			args = append(args, "-s")
		}
	}
	if o := b.cfg.OptLevel; o != "s" && o != "z" {
		// Keep parallel GC in programs that never start a goroutine (rtcore).
		if runtime.GOOS == "darwin" {
			args = append(args, "-Wl,-u,_rt_gc_parallel_enable")
		} else {
			args = append(args, "-Wl,-u,rt_gc_parallel_enable")
		}
	}
	args = append(args, extra...)
	if os.Getenv("MARMOT_LINK_VIA_CLANG") == "" {
		if ld, ok := b.ldCommand(args, objs, out); ok {
			if err := b.run(ld[0], ld[1:]...); err == nil {
				return nil
			}
			// A stale template (SDK or toolchain update): drop it and let
			// clang link, which also reports the real error if any.
			b.dropLdCache()
		}
	}
	return b.run(llvmTool("clang"), args...)
}

// wpo reports whether the backend compiles all modules as one program
// (cross-package inlining, dead code removal): release builds only (
// "Two build modes"). Objects then depend on every module, so the object
// cache key covers all of them.
func (b *build) wpo() bool {
	switch os.Getenv("MARMOT_WPO") {
	case "0":
		return false
	case "1":
		return b.cfg.Emit == "exe"
	}
	return b.cfg.Release && b.cfg.Emit == "exe"
}

// toolTimeout bounds every external tool run.
func toolTimeout() time.Duration {
	if v := os.Getenv("MARMOT_TOOL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 10 * time.Minute
}

// backendArgs are the marmot-backend arguments for a batch of jobs read
// from the file batch ("-": stdin).
func (b *build) backendArgs(emitKind, batch string) []string {
	jobs := maxJobs()
	if batch == "-" {
		jobs = genJobs()
	}
	args := []string{"-O", b.cfg.OptLevel, "--emit=" + emitKind, "--batch=" + batch, "-j", fmt.Sprint(jobs)}
	if b.wpo() {
		args = append(args, "-wpo")
	}
	if b.cfg.PGOGen {
		out := b.cfg.Output
		if out == "" {
			out = "."
		}
		dir, _ := filepath.Abs(filepath.Dir(out))
		args = append(args, "-pgo-gen="+filepath.Join(dir, "marmot-%m.profraw"))
	}
	if b.cfg.PGO != "" {
		p, _ := filepath.Abs(b.cfg.PGO)
		args = append(args, "-pgo="+p)
	}
	if !b.cfg.NoCache {
		args = append(args, "-cache-dir", filepath.Join(b.cfg.Root, "build", "cache", "backend"))
	}
	if bc := b.rtLib("rtcore.bc"); fileExists(bc) {
		args = append(args, "--rt-bitcode="+bc)
	}
	if b.cross() {
		args = append(args, "--target="+b.goos+"/"+b.goarch)
	}
	args = append(args, b.backendBuildArgs(b.cfg.Emit)...)
	if b.cfg.Verbose {
		args = append(args, "-time")
	}
	return args
}

// logBackendTimes prints the backend's phase totals (-time).
func (b *build) logBackendTimes() {
	if !b.cfg.Verbose {
		return
	}
	for _, l := range strings.Split(b.lastOutput, "\n") {
		if strings.HasPrefix(l, "sum of jobs") || strings.HasPrefix(l, "batch:") || strings.HasPrefix(l, "startup") {
			b.logf("backend: %s", strings.Join(strings.Fields(l), " "))
		}
	}
}

func (b *build) run(name string, args ...string) error {
	if b.cfg.PrintCmd {
		fmt.Fprintln(b.cfg.Stderr, name+" "+strings.Join(args, " "))
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	startGroup(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	children.add(cmd.Process.Pid)
	err := cmd.Wait()
	children.remove(cmd.Process.Pid)
	killGroup(cmd.Process.Pid) // stragglers in the tool's group
	outb := outBuf.Bytes()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s timed out after %v", filepath.Base(name), toolTimeout())
	}
	b.lastOutput = string(outb)
	if err != nil {
		msg := strings.TrimSpace(string(outb))
		if len(msg) > 4000 {
			msg = msg[:4000] + "\n..."
		}
		return fmt.Errorf("%s failed: %v\n%s", filepath.Base(name), err, msg)
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// parseTarget turns "-target os/arch" (or "") into GOOS and GOARCH.
func parseTarget(t string) (string, string, error) {
	if t == "" {
		return runtime.GOOS, runtime.GOARCH, nil
	}
	parts := strings.Split(t, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("bad -target %q: want os/arch, for example linux/amd64", t)
	}
	switch t {
	case "darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64":
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("unsupported -target %q (darwin/arm64, darwin/amd64, linux/amd64, linux/arm64)", t)
}

// cross reports whether the target differs from the host.
func (b *build) cross() bool { return b.goos != runtime.GOOS || b.goarch != runtime.GOARCH }

// rtLib returns the runtime core library file for the target.
func (b *build) rtLib(name string) string {
	if b.cross() {
		return filepath.Join(b.cfg.Root, "build", "lib", b.goos+"-"+b.goarch, name)
	}
	return filepath.Join(b.cfg.Root, "build", "lib", name)
}

// linkBundle writes, for a cross build, a directory at out holding the
// objects, the runtime core library and link.sh, to link on the target.
func (b *build) linkBundle(objs []string, rt, out string) error {
	os.RemoveAll(out)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var names []string
	for i, o := range objs {
		name := fmt.Sprintf("m%03d.o", i)
		if err := copyFile(o, filepath.Join(out, name)); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := copyFile(rt, filepath.Join(out, "librtcore.a")); err != nil {
		return err
	}
	exe := filepath.Base(out)
	script := fmt.Sprintf(`#!/bin/sh
# Link the %s/%s program built by marmot: sh link.sh [output]
# The output defaults to %s inside this directory; a relative path is
# relative to the current directory.
set -e
dir="$(cd "$(dirname "$0")" && pwd)"
out="${1:-$dir/%s}"
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac
cd "$dir"
${CC:-clang} -o "$out" %s librtcore.a -lpthread -Wl,--gc-sections -s
`, b.goos, b.goarch, exe, exe, strings.Join(names, " "))
	return os.WriteFile(filepath.Join(out, "link.sh"), []byte(script), 0o755)
}

// linkLibraries turns //go:cgo_import_dynamic library paths into linker
// arguments: -framework NAME for system frameworks, the path itself for
// other dylibs; libSystem is linked anyway. Unused ones are dropped by
// -dead_strip_dylibs.
func linkLibraries(libs []string) []string {
	var args []string
	seen := map[string]bool{}
	for _, l := range libs {
		if strings.Contains(l, "libSystem") || strings.HasPrefix(l, "/usr/lib/libc.") {
			continue
		}
		var a []string
		if i := strings.Index(l, ".framework/"); i >= 0 && runtime.GOOS == "darwin" {
			name := filepath.Base(l[:i])
			a = []string{"-framework", name}
		} else if runtime.GOOS == "darwin" && strings.HasPrefix(l, "/usr/lib/lib") {
			// /usr/lib/libresolv.9.dylib -> -lresolv (the file is only in
			// the dyld shared cache).
			name := strings.TrimPrefix(filepath.Base(l), "lib")
			if i := strings.IndexByte(name, '.'); i > 0 {
				name = name[:i]
			}
			a = []string{"-l" + name}
		} else {
			continue // other platforms: libc only for now
		}
		k := strings.Join(a, " ")
		if !seen[k] {
			seen[k] = true
			args = append(args, a...)
		}
	}
	if len(args) > 0 && runtime.GOOS == "darwin" {
		args = append(args, "-Wl,-dead_strip_dylibs")
	}
	return args
}

// expandParts replaces an object the backend split into groups by the
// objects listed in its OUT.parts file (OUT first; "Large
// modules").
func expandParts(objs []string) []string {
	var out []string
	for _, o := range objs {
		data, err := os.ReadFile(o + ".parts")
		if err != nil {
			out = append(out, o)
			continue
		}
		for _, l := range strings.Split(string(data), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				out = append(out, l)
			}
		}
	}
	return out
}

// xdefs are the string variables set at build time, as go's linker -X:
// test binaries report testing.Testing() true, as go test builds them.
func (b *build) xdefs() map[string]string {
	if b.cfg.Test {
		return map[string]string{"testing.testBinary": "1"}
	}
	return nil
}

// writeXDefs adds the build-time string variables to a cache key.
func (b *build) writeXDefs(w io.Writer) {
	x := b.xdefs()
	keys := make([]string, 0, len(x))
	for k := range x {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "X %s=%q\n", k, x[k])
	}
}

// moduleTextVersion changes when the driver changes module text after
// codegen (aliasTypes), so keys made from package keys change with it. The
// work file and the cached file of a module have the same text.
const moduleTextVersion = "t1"

// Package driver runs the marmot pipeline: load, SSA, MLIR codegen per
// package, backend compilation to objects (cached), and linking.
package driver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/codegen"
	"github.com/Gaurav-Gosain/marmot/internal/loader"
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
	cfg   Config
	work  string
	start time.Time
}

func (b *build) logf(format string, args ...any) {
	if b.cfg.Verbose {
		fmt.Fprintf(b.cfg.Stderr, "marmot: %6.0fms "+format+"\n", append([]any{float64(time.Since(b.start).Microseconds()) / 1000}, args...)...)
	}
}

// Build compiles the packages and writes the output. It returns the output
// path.
func Build(cfg Config) (string, error) {
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	b := &build{cfg: cfg, start: time.Now()}
	work, err := os.MkdirTemp("", "marmot-")
	if err != nil {
		return "", err
	}
	b.work = work
	if cfg.Work {
		fmt.Fprintf(cfg.Stderr, "WORK=%s\n", work)
	} else {
		defer os.RemoveAll(work)
		cleanupDirs.add(work)
		defer cleanupDirs.remove(work)
	}
	stop := handleSignals()
	defer stop()

	goos, goarch := runtime.GOOS, runtime.GOARCH
	stamp := ""
	if cfg.Output != "" && cfg.Emit == "exe" {
		stamp = b.buildStamp(goos, goarch)
		if b.upToDate(stamp, cfg.Output) {
			b.logf("up to date (stamp %s)", stamp)
			return cfg.Output, nil
		}
	}
	lp, err := loader.Load(loader.Config{
		Dir: cfg.Dir, Root: cfg.Root, Tags: cfg.Tags, GOOS: goos, GOARCH: goarch, Patterns: cfg.Patterns,
		Log: b.logf,
	})
	if err != nil {
		return "", err
	}
	b.logf("loaded %d packages", len(lp.All))
	if len(lp.Packages) != 1 {
		return "", fmt.Errorf("build of %d packages is not supported; name one main package", len(lp.Packages))
	}
	root := lp.Packages[0]
	if root.Name != "main" && cfg.Emit == "exe" {
		return "", fmt.Errorf("%s is not a main package", root.PkgPath)
	}

	files := map[*types.Package][]*ast.File{}
	var ssaPkgs []*ssa.Package
	for _, p := range lp.All {
		files[p.Types] = p.Syntax
		if sp := lp.SSAPkgs[p.Types]; sp != nil {
			ssaPkgs = append(ssaPkgs, sp)
		}
	}
	cg, err := codegen.NewProgram(lp.SSA, ssaPkgs, files, codegen.Options{GOOS: goos, GOARCH: goarch, Sizes: lp.Sizes, GoDialect: cfg.Dialect == "go"})
	if err != nil {
		return "", err
	}
	mainSSA := lp.SSAPkgs[root.Types]

	// Generate MLIR for every package in parallel.
	pkgs := cg.Packages()
	mods := make([]codegen.Module, len(pkgs)+1)
	errs := make([]error, len(pkgs)+1)
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i, p := range pkgs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mods[i], errs[i] = cg.Generate(p)
		}()
	}
	if root.Name == "main" {
		mods[len(pkgs)], errs[len(pkgs)] = cg.GenerateMain(mainSSA)
	} else {
		mods = mods[:len(pkgs)]
	}
	wg.Wait()
	var msgs []string
	for _, e := range errs {
		if e != nil {
			msgs = append(msgs, e.Error())
		}
	}
	if len(msgs) > 0 {
		return "", fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}
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

	objs, err := b.compile(mods, cfg.Emit)
	if err != nil {
		return "", err
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
	if err := b.link(objs, out); err != nil {
		return "", err
	}
	b.logf("linked %s", out)
	b.saveStamp(stamp, out)
	return out, nil
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
	haveBackend := fileExists(backend) && os.Getenv("MARMOT_NO_BACKEND") == ""
	if b.cfg.Dialect == "go" && !haveBackend {
		return nil, fmt.Errorf("-dialect=go needs build/bin/marmot-backend")
	}
	ext := ".o"
	if emit == "llvm" {
		ext = ".ll"
	}
	cacheDir := filepath.Join(b.cfg.Root, "build", "cache")
	os.MkdirAll(cacheDir, 0o755)
	objs := make([]string, len(mods))
	type job struct{ in, out, cached string }
	var jobs []job
	for i, m := range mods {
		h := sha256.New()
		fmt.Fprintf(h, "v1\n%s\n%s\n%v\n%s\n", b.cfg.OptLevel, emit, haveBackend, b.cfg.Dialect)
		if haveBackend {
			if fi, err := os.Stat(backend); err == nil {
				fmt.Fprintf(h, "%d %d\n", fi.Size(), fi.ModTime().UnixNano())
			}
		}
		io.WriteString(h, m.Text)
		key := hex.EncodeToString(h.Sum(nil))[:32]
		cached := filepath.Join(cacheDir, key+ext)
		in := filepath.Join(b.work, fmt.Sprintf("m%03d.mlir", i))
		out := filepath.Join(b.work, fmt.Sprintf("m%03d%s", i, ext))
		if b.cfg.Work {
			os.WriteFile(in, []byte("// "+m.Name+"\n"+m.Text), 0o644)
		}
		if !b.cfg.NoCache && fileExists(cached) {
			objs[i] = cached
			continue
		}
		if err := os.WriteFile(in, []byte(m.Text), 0o644); err != nil {
			return nil, err
		}
		objs[i] = out
		jobs = append(jobs, job{in, out, cached})
	}
	b.logf("object cache: %d hits, %d misses", len(mods)-len(jobs), len(jobs))
	if b.cfg.Verbose {
		for _, j := range jobs {
			for i, m := range mods {
				if objs[i] == j.out {
					b.logf("  compile %s", m.Name)
				}
			}
		}
	}
	if len(jobs) == 0 {
		return objs, nil
	}
	if haveBackend {
		var list strings.Builder
		for _, j := range jobs {
			fmt.Fprintf(&list, "%s %s\n", j.in, j.out)
		}
		listFile := filepath.Join(b.work, "jobs.txt")
		os.WriteFile(listFile, []byte(list.String()), 0o644)
		emitKind := "obj"
		if emit == "llvm" {
			emitKind = "llvm"
		}
		args := []string{"-O", b.cfg.OptLevel, "--emit=" + emitKind, "--batch=" + listFile, "-j", fmt.Sprint(maxJobs())}
		if bc := filepath.Join(b.cfg.Root, "build", "lib", "rtcore.bc"); fileExists(bc) {
			args = append(args, "--rt-bitcode="+bc)
		}
		if err := b.run(backend, args...); err != nil {
			return nil, err
		}
	} else {
		var wg sync.WaitGroup
		errs := make([]error, len(jobs))
		sem := make(chan struct{}, maxJobs())
		for i, j := range jobs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ll := strings.TrimSuffix(j.in, ".mlir") + ".ll"
				if emit == "llvm" {
					ll = j.out
				}
				if err := b.run(llvmTool("mlir-translate"), "--mlir-to-llvmir", j.in, "-o", ll); err != nil {
					errs[i] = err
					return
				}
				if emit != "llvm" {
					errs[i] = b.run(llvmTool("clang"), "-c", "-O"+b.cfg.OptLevel, "-Wno-override-module", "-o", j.out, "-x", "ir", ll)
				}
			}()
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				return nil, e
			}
		}
	}
	for _, j := range jobs {
		if !b.cfg.NoCache {
			tmp := j.cached + ".tmp"
			if copyFile(j.out, tmp) == nil {
				os.Rename(tmp, j.cached)
			}
		}
	}
	return objs, nil
}

func (b *build) link(objs []string, out string) error {
	rt := filepath.Join(b.cfg.Root, "build", "lib", "librtcore.a")
	if !fileExists(rt) {
		return fmt.Errorf("%s is missing; run make rtcore", rt)
	}
	args := []string{"-o", out}
	args = append(args, objs...)
	args = append(args, rt, "-lpthread")
	if mp := os.Getenv("MARMOT_LINKMAP"); mp != "" && runtime.GOOS == "darwin" {
		args = append(args, "-Wl,-map,"+mp)
	}
	if runtime.GOOS == "darwin" {
		// Only main is exported: every other symbol becomes local and -x
		// drops it from the symbol table (Go symbol names are long).
		args = append(args, "-Wl,-dead_strip")
		if os.Getenv("MARMOT_KEEP_SYMBOLS") == "" {
			args = append(args, "-Wl,-x", "-Wl,-exported_symbol,_main")
		}
	} else {
		args = append(args, "-Wl,--gc-sections", "-s")
	}
	return b.run(llvmTool("clang"), args...)
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

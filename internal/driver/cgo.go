package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/cgo"
	"github.com/Gaurav-Gosain/marmot/internal/sysroot"
)

// cgo packages: the C files internal/cgo generates (the wrappers of the C
// functions the Go code calls) and the package's own .c, .s and .S files
// are compiled with clang, with the package's #cgo CFLAGS, and cached by
// content. Their objects and #cgo LDFLAGS join the link. Release builds also
// compile the C files to bitcode and give it to the backend with the
// runtime's bitcode, so small C functions (and the runtime's cgo entry and
// exit) inline into the Go code that calls them.

// prepareCgo compiles the C side of the cgo packages before code generation.
func (b *build) prepareCgo(pkgs map[string]*cgo.Package) error {
	objs, bcs, ldflags, err := b.cgoObjects(pkgs, b.cfg.Release)
	if err != nil {
		return err
	}
	b.cgoObjs, b.cgoLDFLAGS = objs, ldflags
	if len(bcs) == 0 {
		return nil
	}
	rt := b.rtLib("rtcore.bc")
	if !fileExists(rt) {
		return nil
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", statSig(rt))
	for _, bc := range bcs {
		fmt.Fprintf(h, "%s\n", bc) // content keyed
	}
	out := filepath.Join(b.cfg.Root, "build", "cache", "cgo-obj", hex.EncodeToString(h.Sum(nil))[:32]+".rt.bc")
	if !fileExists(out) {
		tmp := out + fmt.Sprintf(".%d.tmp", os.Getpid())
		if err := b.run(llvmTool("llvm-link"), append([]string{rt, "-o", tmp}, bcs...)...); err != nil {
			_ = os.Remove(tmp) // best effort: the temp file of a failed step
			return err
		}
		if err := os.Rename(tmp, out); err != nil {
			return err
		}
	}
	b.rtBitcode = out
	return nil
}

// cgoObjects compiles the C side of the cgo packages: objects, and with
// bitcode set also bitcode of the C files.
func (b *build) cgoObjects(pkgs map[string]*cgo.Package, bitcode bool) ([]string, []string, []string, error) {
	if len(pkgs) == 0 {
		return nil, nil, nil, nil
	}
	target, err := b.cgoTargetFlags()
	if err != nil {
		return nil, nil, nil, err
	}
	var paths []string
	for p := range pkgs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	type job struct {
		src   string
		flags []string
		obj   string
		bc    bool
		pkg   string
	}
	var jobs []job
	var ldflags []string
	cc := llvmTool("clang")
	ccSig := statSig(cc)
	cacheDir := filepath.Join(b.cfg.Root, "build", "cache", "cgo-obj")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, nil, nil, err
	}
	opt := "-O2"
	for _, path := range paths {
		p := pkgs[path]
		headers := headerSig(p.Dir) + headerSig(p.OutDir)
		flags := append(append(append([]string{}, target...), opt, "-fno-omit-frame-pointer", "-fno-stack-protector", "-w"), p.CFLAGS...)
		for _, src := range append(append([]string{}, p.CFiles...), p.SFiles...) {
			data, err := os.ReadFile(src)
			if err != nil {
				return nil, nil, nil, err
			}
			h := sha256.New()
			fmt.Fprintf(h, "%s\n%s\n%q\n%s/%s\n%s\n", ccSig, filepath.Base(src), flags, b.goos, b.goarch, headers)
			h.Write(data)
			key := hex.EncodeToString(h.Sum(nil))[:32]
			jobs = append(jobs, job{src, flags, filepath.Join(cacheDir, key+".o"), false, path})
			if bitcode && strings.HasSuffix(src, ".c") {
				jobs = append(jobs, job{src, flags, filepath.Join(cacheDir, key+".bc"), true, path})
			}
		}
		ldflags = append(ldflags, p.LDFLAGS...)
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	errs := make([]error, len(jobs))
	for i, j := range jobs {
		if fileExists(j.obj) {
			continue
		}
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tmp := j.obj + fmt.Sprintf(".%d.tmp", os.Getpid())
			args := append(append([]string{"-c"}, j.flags...), j.src, "-o", tmp)
			if j.bc {
				args = append([]string{"-emit-llvm"}, args...)
			}
			if err := b.compileC(cc, args); err != nil {
				errs[i] = err
				_ = os.Remove(tmp) // best effort: the temp file of a failed step
				return
			}
			errs[i] = os.Rename(tmp, j.obj)
		}(i, j)
	}
	wg.Wait()
	var objs, bcs []string
	for i, j := range jobs {
		if errs[i] != nil {
			return nil, nil, nil, errs[i]
		}
		if j.bc {
			bcs = append(bcs, j.obj)
		} else {
			objs = append(objs, j.obj)
			if b.cgoObjPkg == nil {
				b.cgoObjPkg = map[string]string{}
			}
			b.cgoObjPkg[j.obj] = j.pkg
		}
	}
	return objs, bcs, ldflags, nil
}

// cgoTriples are the clang targets of cross builds (as runtime/core's
// Makefile).
var cgoTriples = map[string]string{
	"linux/amd64":  "x86_64-linux-gnu",
	"linux/arm64":  "aarch64-linux-gnu",
	"darwin/amd64": "x86_64-apple-macosx11.0",
	"darwin/arm64": "arm64-apple-macosx11.0",
}

// cgoTargetFlags are the clang flags of the target for C code: none for a
// native build; --target and --sysroot for a cross build. A Linux target
// needs its C headers and libraries: MARMOT_SYSROOT names that tree (a copy
// of the target's /usr/include and /usr/lib is enough).
func (b *build) cgoTargetFlags() ([]string, error) {
	if !b.cross() && !b.cfg.Static {
		return nil, nil
	}
	triple := b.cTriple()
	if triple == "" {
		return nil, fmt.Errorf("cgo: no C target for %s/%s", b.goos, b.goarch)
	}
	flags := []string{"--target=" + triple}
	if b.cfg.Static {
		sr := b.crossSysroot()
		if sr == "" {
			return nil, fmt.Errorf("cgo: -static needs the musl sysroot: run marmot sysroot add -libc musl %s/%s", b.goos, b.goarch)
		}
		return append(flags, "--sysroot="+sr), nil
	}
	switch sr := os.Getenv("MARMOT_SYSROOT"); {
	case sr != "":
		flags = append(flags, "--sysroot="+sr)
	case sysroot.Installed(b.cfg.Root, b.goos, b.goarch):
		flags = append(flags, "--sysroot="+sysroot.Dir(b.cfg.Root, b.goos, b.goarch))
	case b.goos != runtime.GOOS:
		return nil, fmt.Errorf("cgo: cross compiling C for %s/%s needs its headers: run marmot sysroot add %s/%s (or set MARMOT_SYSROOT to a copy of the target's root)", b.goos, b.goarch, b.goos, b.goarch)
	}
	return flags, nil
}

// errC is an error in the user's C code: clang's diagnostics as text, so
// they read as build errors of the program (M0030), not of marmot.
type errC struct{ text string }

func (e *errC) Error() string { return e.text }

// compileC runs clang on a C or assembly file of a cgo package. A failure
// with diagnostics is the program's error (errC).
func (b *build) compileC(cc string, args []string) error {
	if b.cfg.PrintCmd {
		fmt.Fprintln(b.cfg.Stderr, cc+" "+strings.Join(args, " "))
	}
	out, err := exec.Command(cc, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if text := strings.TrimSpace(string(out)); strings.Contains(text, ": error: ") {
		return &errC{text}
	}
	return fmt.Errorf("%s: %v: %s", cc, err, out)
}

// headerSig is a signature of the .h files in dir.
func headerSig(dir string) string {
	if dir == "" {
		return ""
	}
	ents, _ := os.ReadDir(dir)
	var b strings.Builder
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".h") {
			b.WriteString(statSig(filepath.Join(dir, e.Name())) + "\n")
		}
	}
	return b.String()
}

func statSig(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return path + " missing"
	}
	return fmt.Sprintf("%s %d %d", path, st.Size(), st.ModTime().UnixNano())
}

// cgoLoaderFlags are the target flags for cgo's own use of clang (its
// type probes): those of cgoTargetFlags, or none when they are not
// available (the build then fails at the C compile with the reason).
func (b *build) cgoLoaderFlags() []string {
	f, _ := b.cgoTargetFlags()
	return f
}

// shellArgs quotes args for a sh command line, each after a space.
func shellArgs(args []string) string {
	var s strings.Builder
	for _, a := range args {
		s.WriteString(" '" + strings.ReplaceAll(a, "'", `'\''`) + "'")
	}
	return s.String()
}

// LLVMTool is the path of an LLVM tool marmot uses (clang, llvm-link).
func LLVMTool(name string) string { return llvmTool(name) }

// Package cgo turns the cgo files of a package into Go and C that marmot
// compiles, with C calls that are direct native calls.
//
// cmd/cgo itself does the cgo work, in gccgo mode: it reads the preamble,
// learns the C types from clang's DWARF and applies all of cgo's rules
// (type mapping, #define constants, errno results, C.CString and friends).
// Its gccgo output declares each C function as a //extern Go function of a
// thin C wrapper with the C ABI, which is what marmot wants: no argument
// frame and no stack switch. This package rewrites that output:
//
//   - //extern W becomes //go:linkname W W: marmot calls body-less linknamed
//     functions with the C ABI.
//   - The C wrappers bracket the call with rt_cgo_enter and rt_cgo_exit
//     (entersyscall, and the "in C" flag), or with the _fast variants (the
//     flag only) for functions that do not block: libc leaf functions and
//     functions marked with both #cgo noescape and #cgo nocallback.
//   - Arguments and results of aggregate type (structs, unions, arrays,
//     complex numbers) go through a C adapter that takes pointers: LLVM's
//     lowering of a struct argument is not the C ABI.
//   - C.CString, C.GoString, C.GoStringN, C.GoBytes, C.CBytes and C.malloc
//     are written in Go (cgo's gccgo prolog calls libgo functions).
//   - Functions called as r, err := C.f() keep errno in the goroutine
//     (rt_cgo_exit_errno), since the goroutine may change threads before Go
//     reads it.
//   - C variables (C.v) are pointers set from a C getter at package init.
package cgo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// version changes the cache key when the generated code changes.
const version = "marmot-cgo-4"

// Config describes the build a package is prepared for.
type Config struct {
	CC         string // the C compiler (clang)
	GOOS       string
	GOARCH     string
	Tags       []string // build tags
	RealGOROOT string   // the Go toolchain whose cmd/cgo runs
	CacheDir   string   // generated files go to CacheDir/<key>
}

// Package is a cgo package prepared for marmot.
type Package struct {
	ImportPath string
	Dir        string   // the package directory
	OutDir     string   // where the generated files are
	GoFiles    []string // every Go file to compile: the package's plain Go files and the generated ones
	Imports    []string // import paths of GoFiles
	CFiles     []string // C files to compile: generated wrappers and the package's .c files
	SFiles     []string // assembly files to compile (.S and .s, preprocessed by clang)
	CFLAGS     []string // CPPFLAGS and CFLAGS, pkg-config included
	LDFLAGS    []string
}

// Context returns the go/build context of cfg with cgo on.
func (cfg Config) Context() build.Context {
	ctxt := build.Default
	ctxt.GOOS, ctxt.GOARCH = cfg.GOOS, cfg.GOARCH
	ctxt.CgoEnabled = true
	ctxt.BuildTags = cfg.Tags
	ctxt.GOROOT = cfg.RealGOROOT
	return ctxt
}

// Import is go/build's view of the package in dir with cgo on; with files
// set, of those files only.
func (cfg Config) Import(dir string, files []string) (*build.Package, error) {
	ctxt := cfg.Context()
	if files != nil {
		keep := map[string]bool{}
		for _, f := range files {
			keep[filepath.Base(f)] = true
		}
		ctxt.ReadDir = func(d string) ([]fs.FileInfo, error) {
			ents, err := os.ReadDir(d)
			if err != nil {
				return nil, err
			}
			var out []fs.FileInfo
			for _, e := range ents {
				if d != dir || keep[e.Name()] {
					if fi, err := e.Info(); err == nil {
						out = append(out, fi)
					}
				}
			}
			return out, nil
		}
	}
	return ctxt.ImportDir(dir, 0)
}

// Prepare runs cgo on the package in dir. With files set, the package is
// only those files of dir (go build file.go). It returns nil, nil when the
// package has no cgo files.
func Prepare(cfg Config, dir, importPath string, files []string) (*Package, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	bp, err := cfg.Import(dir, files)
	if err != nil {
		if _, ok := err.(*build.NoGoError); !ok {
			return nil, err
		}
	}
	if bp == nil || len(bp.CgoFiles) == 0 {
		return nil, nil
	}
	p := &Package{ImportPath: importPath, Dir: dir}
	src := "#cgo directives of " + importPath
	if err := checkCompilerFlags("CPPFLAGS", src, bp.CgoCPPFLAGS); err != nil {
		return nil, err
	}
	if err := checkCompilerFlags("CFLAGS", src, bp.CgoCFLAGS); err != nil {
		return nil, err
	}
	if err := checkLinkerFlags("LDFLAGS", src, bp.CgoLDFLAGS); err != nil {
		return nil, err
	}
	flags := append(append([]string{}, bp.CgoCPPFLAGS...), bp.CgoCFLAGS...)
	p.LDFLAGS = append([]string{}, bp.CgoLDFLAGS...)
	if len(bp.CgoPkgConfig) > 0 {
		cf, lf, err := pkgConfig(bp.CgoPkgConfig)
		if err != nil {
			return nil, err
		}
		flags = append(flags, cf...)
		p.LDFLAGS = append(p.LDFLAGS, lf...)
	}

	// The key covers the inputs cgo reads.
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s/%s\n%q\n%q\n", version, importPath, cfg.GOOS, cfg.GOARCH, flags, cfg.Tags)
	fmt.Fprintf(h, "%s\n", fileSig(cfg.CC))
	fmt.Fprintf(h, "%s\n", fileSig(cgoTool(cfg.RealGOROOT)))
	// The rewrite is part of marmot: a new marmot generates again.
	if exe, err := os.Executable(); err == nil {
		fmt.Fprintf(h, "%s\n", fileSig(exe))
	}
	var inputs []string
	for _, f := range bp.CgoFiles {
		inputs = append(inputs, filepath.Join(dir, f))
	}
	for _, f := range bp.HFiles {
		inputs = append(inputs, filepath.Join(dir, f))
	}
	for _, f := range inputs {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(h, "%s %x\n", filepath.Base(f), sha256.Sum256(data))
	}
	key := hex.EncodeToString(h.Sum(nil))[:24]
	p.OutDir = filepath.Join(cfg.CacheDir, key)
	// The package's C files may include _cgo_export.h.
	p.CFLAGS = append(flags, "-I", dir, "-I", p.OutDir)

	if _, err := os.Stat(filepath.Join(p.OutDir, "done")); err != nil {
		if err := generate(cfg, bp, dir, importPath, flags, p.OutDir); err != nil {
			return nil, err
		}
	}

	for _, f := range bp.GoFiles {
		p.GoFiles = append(p.GoFiles, filepath.Join(dir, f))
	}
	p.GoFiles = append(p.GoFiles, filepath.Join(p.OutDir, "_cgo_gotypes.go"))
	for _, f := range bp.CgoFiles {
		base := strings.TrimSuffix(f, ".go")
		p.GoFiles = append(p.GoFiles, filepath.Join(p.OutDir, base+".cgo1.go"))
		p.CFiles = append(p.CFiles, filepath.Join(p.OutDir, base+".cgo2.c"))
	}
	if _, err := os.Stat(filepath.Join(p.OutDir, "_cgo_export.c")); err == nil {
		p.CFiles = append(p.CFiles, filepath.Join(p.OutDir, "_cgo_export.c"))
	}
	for _, f := range bp.CFiles {
		p.CFiles = append(p.CFiles, filepath.Join(dir, f))
	}
	for _, f := range bp.SFiles {
		p.SFiles = append(p.SFiles, filepath.Join(dir, f))
	}
	imports := map[string]bool{"unsafe": true, "syscall": true}
	for _, i := range bp.Imports {
		if i != "C" {
			imports[i] = true
		}
	}
	for i := range imports {
		p.Imports = append(p.Imports, i)
	}
	sort.Strings(p.Imports)
	return p, nil
}

func fileSig(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return path + " missing"
	}
	return fmt.Sprintf("%s %d %d", path, st.Size(), st.ModTime().UnixNano())
}

func cgoTool(goroot string) string {
	return filepath.Join(goroot, "pkg", "tool", runtime.GOOS+"_"+runtime.GOARCH, "cgo")
}

func pkgConfig(pkgs []string) (cflags, ldflags []string, err error) {
	var flags, names []string
	for _, a := range pkgs {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else if !validPkgConfigName(a) {
			return nil, nil, fmt.Errorf("invalid pkg-config package name: %s", a)
		} else {
			names = append(names, a)
		}
	}
	if err := checkPkgConfigFlags("", "pkg-config", flags); err != nil {
		return nil, nil, err
	}
	pkgs = append(append([]string{}, flags...), names...)
	defer func() {
		if err == nil {
			if e := checkCompilerFlags("CFLAGS", "pkg-config --cflags", cflags); e != nil {
				err = e
			} else if e := checkLinkerFlags("LDFLAGS", "pkg-config --libs", ldflags); e != nil {
				err = e
			}
		}
	}()
	out, err := exec.Command("pkg-config", append([]string{"--cflags"}, pkgs...)...).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("pkg-config --cflags %s: %v", strings.Join(pkgs, " "), err)
	}
	cflags = strings.Fields(string(out))
	out, err = exec.Command("pkg-config", append([]string{"--libs"}, pkgs...)...).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("pkg-config --libs %s: %v", strings.Join(pkgs, " "), err)
	}
	return cflags, strings.Fields(string(out)), nil
}

// generate runs cgo into a temporary directory, rewrites its output and
// moves it to out.
func generate(cfg Config, bp *build.Package, dir, importPath string, flags []string, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(out), "tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // temporary directory; a leftover only costs disk
	// cgo asks gccgo once how it mangles symbols; marmot does not use the
	// mangled names, so a stub that answers with the current scheme does.
	stub := filepath.Join(tmp, "gccgo-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho go_0l_u00e4ufer.Run\n"), 0o755); err != nil {
		return err
	}
	pkgpath := importPath
	if bp.Name == "main" {
		pkgpath = "main"
	}
	args := []string{"-gccgo", "-gccgopkgpath=" + pkgpath, "-objdir", tmp, "-importpath", importPath, "-srcdir", dir, "--"}
	args = append(args, flags...)
	args = append(args, bp.CgoFiles...)
	cmd := exec.Command(cgoTool(cfg.RealGOROOT), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CC="+cfg.CC, "GCCGO="+stub, "GOOS="+cfg.GOOS, "GOARCH="+cfg.GOARCH,
		"CGO_ENABLED=1", "GOROOT="+cfg.RealGOROOT)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	marks := directives(bp, dir)
	if err := rewrite(tmp, bp, marks); err != nil {
		return fmt.Errorf("cgo output of %s: %v", importPath, err)
	}
	fastLeaves(tmp, cfg.CC, append(append([]string{}, flags...), "-I", dir))
	// Keep only what marmot compiles.
	for _, f := range []string{"_cgo_defun.c", "_cgo_main.c", "_cgo_flags", "gccgo-stub"} {
		_ = os.Remove(filepath.Join(tmp, f)) // unused cgo output; a leftover only costs disk
	}
	ents, _ := os.ReadDir(tmp)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".o") {
			_ = os.Remove(filepath.Join(tmp, e.Name())) // gcc objects marmot does not use
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "done"), nil, 0o644); err != nil {
		return err
	}
	_ = os.RemoveAll(out) // a stale directory; if it stays, the Rename below fails and says so
	return os.Rename(tmp, out)
}

// marks are the #cgo noescape and nocallback names of a package.
type marks struct{ noescape, nocallback map[string]bool }

var reDirective = regexp.MustCompile(`(?m)^\s*#cgo\s+(noescape|nocallback)\s+(\w+)`)

func directives(bp *build.Package, dir string) marks {
	m := marks{map[string]bool{}, map[string]bool{}}
	fset := token.NewFileSet()
	for _, name := range bp.CgoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			continue
		}
		for _, imp := range f.Imports {
			if imp.Path.Value != `"C"` {
				continue
			}
			doc := imp.Doc
			if doc == nil {
				for _, d := range f.Decls {
					if g, ok := d.(*ast.GenDecl); ok && len(g.Specs) == 1 && g.Specs[0] == imp {
						doc = g.Doc
					}
				}
			}
			if doc == nil {
				continue
			}
			for _, sm := range reDirective.FindAllStringSubmatch(doc.Text(), -1) {
				if sm[1] == "noescape" {
					m.noescape[sm[2]] = true
				} else {
					m.nocallback[sm[2]] = true
				}
			}
		}
	}
	return m
}

// leaf lists C library functions that return quickly and never call back
// into Go: their calls skip the syscall bracketing.
var leaf = map[string]bool{}

func init() {
	for n := range strings.FieldsSeq(`
		strlen strnlen strcmp strncmp strcasecmp strncasecmp strchr strrchr strstr strspn strcspn strpbrk
		strcpy strncpy strcat strncat strdup strndup memcpy memmove memset memcmp memchr
		malloc calloc realloc free
		abs labs llabs div ldiv
		isalnum isalpha isdigit isxdigit islower isupper isspace ispunct isprint iscntrl isgraph tolower toupper
		sqrt sqrtf cbrt sin cos tan asin acos atan atan2 sinh cosh tanh exp exp2 expm1 log log2 log10 log1p
		pow fabs fabsf floor ceil round trunc fmod hypot fmin fmax fma sinf cosf tanf expf logf powf floorf ceilf
		atoi atol atoll strtol strtoul strtoll strtoull strtod strtof
		getpid`) {
		leaf[n] = true
	}
}

// fast reports whether calls of the C function name skip the syscall
// bracketing.
func (m marks) fast(name string) bool {
	return leaf[name] || m.noescape[name] && m.nocallback[name]
}

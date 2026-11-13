package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/cgo"
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
			os.Remove(tmp)
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
	if b.cross() {
		return nil, nil, nil, fmt.Errorf("cgo packages cannot be cross compiled yet")
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
		flags := append([]string{opt, "-fno-omit-frame-pointer", "-fno-stack-protector", "-w"}, p.CFLAGS...)
		for _, src := range append(append([]string{}, p.CFiles...), p.SFiles...) {
			data, err := os.ReadFile(src)
			if err != nil {
				return nil, nil, nil, err
			}
			h := sha256.New()
			fmt.Fprintf(h, "%s\n%s\n%q\n%s/%s\n%s\n", ccSig, filepath.Base(src), flags, b.goos, b.goarch, headers)
			h.Write(data)
			key := hex.EncodeToString(h.Sum(nil))[:32]
			jobs = append(jobs, job{src, flags, filepath.Join(cacheDir, key+".o"), false})
			if bitcode && strings.HasSuffix(src, ".c") {
				jobs = append(jobs, job{src, flags, filepath.Join(cacheDir, key+".bc"), true})
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
			if err := b.run(cc, args...); err != nil {
				errs[i] = fmt.Errorf("%s: %v", j.src, err)
				os.Remove(tmp)
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
		}
	}
	return objs, bcs, ldflags, nil
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

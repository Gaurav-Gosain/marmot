// Command stdobj makes the browser playground's prebuilt standard library for Run: the
// objects of every package the playground offers (deploy/play/bundle.Packages and their
// dependencies), compiled for wasip1/wasm by marmot in one mode, in one archive, with the
// runtime's librtcore.a beside it. In the page, the program's own package is compiled by the
// backend and linked with these by wasm-ld; the linker takes from the archive only the
// packages the program uses.
//
//	go run ./deploy/play/stdobj -marmot build/bin/marmot -mode dev -o out/
//
// The objects come from a separate compilation build of a program that imports every package
// (so the program flags are those of the largest program: methods kept for reflection), with
// tier 1 off and no cache, so they are the tier 0 objects the page's backend makes too.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// packages: keep in step with deploy/play/bundle.
var packages = strings.Fields(`
	bufio bytes cmp container/heap container/list container/ring context crypto/md5
	crypto/sha256 encoding/base64 encoding/binary encoding/csv encoding/hex encoding/json
	errors fmt hash/crc32 hash/fnv io iter maps math math/big math/bits math/cmplx math/rand
	math/rand/v2 os path reflect regexp runtime slices sort strconv strings sync sync/atomic
	text/tabwriter text/template time unicode unicode/utf16 unicode/utf8 unique`)

func main() {
	marmot := flag.String("marmot", "marmot", "the marmot command")
	root := flag.String("root", "", "MARMOT_ROOT (default: marmot's own)")
	mode := flag.String("mode", "dev", "build mode: dev or O2")
	out := flag.String("o", "play-std", "output directory")
	llvmBin := flag.String("llvm", "", "directory with llvm-ar and llvm-nm (default: on PATH)")
	flag.Parse()
	tool := func(n string) string {
		if *llvmBin != "" {
			return filepath.Join(*llvmBin, n)
		}
		return n
	}
	dir, err := os.MkdirTemp("", "play-stdobj-")
	check(err)
	defer os.RemoveAll(dir)
	var src bytes.Buffer
	src.WriteString("package main\n\nimport (\n")
	for _, p := range packages {
		fmt.Fprintf(&src, "\t_ %q\n", p)
	}
	src.WriteString(")\n\nfunc main() {}\n")
	check(os.WriteFile(filepath.Join(dir, "main.go"), src.Bytes(), 0o644))
	check(os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module play\n\ngo 1.24\n"), 0o644))
	args := []string{"build", "-target", "wasip1/wasm", "-work", "-nocache", "-v", "-o", filepath.Join(dir, "prog.wasm")}
	switch *mode {
	case "dev":
	case "O2":
		args = append(args, "-O", "2")
	default:
		fail(fmt.Errorf("-mode %q: dev or O2", *mode))
	}
	cmd := exec.Command(*marmot, append(args, ".")...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MARMOT_TIER1=0", "MARMOT_OUTPUT=plain", "CGO_ENABLED=0")
	if *root != "" {
		cmd.Env = append(cmd.Env, "MARMOT_ROOT="+*root)
	}
	log, err := cmd.CombinedOutput()
	if err != nil {
		fail(fmt.Errorf("%v\n%s", err, log))
	}
	var work, flags string
	for _, l := range strings.Split(string(log), "\n") {
		if w, ok := strings.CutPrefix(l, "WORK="); ok {
			work = strings.TrimSpace(w)
		}
		if i := strings.Index(l, "program flags "); i >= 0 {
			flags = strings.TrimSpace(l[i:])
		}
	}
	if work == "" {
		fail(fmt.Errorf("no WORK= line in the build output:\n%s", log))
	}
	defer os.RemoveAll(work)
	objs, err := filepath.Glob(filepath.Join(work, "m*.o"))
	check(err)
	sort.Strings(objs)
	var std []string
	for _, o := range objs {
		syms, err := exec.Command(tool("llvm-nm"), "--defined-only", "-j", o).Output()
		check(err)
		if bytes.Contains(append([]byte("\n"), syms...), []byte("\nmain.main\n")) {
			continue // the program's own package
		}
		std = append(std, o)
	}
	check(os.MkdirAll(*out, 0o755))
	ar := filepath.Join(*out, "std-"+*mode+".a")
	os.Remove(ar)
	cmd = exec.Command(tool("llvm-ar"), append([]string{"rcs", ar}, std...)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		fail(fmt.Errorf("llvm-ar: %v\n%s", err, b))
	}
	fmt.Printf("%s: %d objects of %d (the program's own left out); %s\n", ar, len(std), len(objs), flags)
}

func check(err error) {
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "stdobj:", err)
	os.Exit(1)
}

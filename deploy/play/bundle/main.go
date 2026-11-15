// Command bundle makes the browser playground's file system: one tar with
//
//	/goroot   marmot's merged GOROOT (Go's src plus lib/src), only the standard packages
//	          the playground offers and their dependencies, for wasip1/wasm
//	/root     a marmot root for MARMOT_ROOT: build/cache/types holds the export data of
//	          those packages, made by the browser's own marmot.wasm (run here under node),
//	          so the browser type-checks only the program and never the standard library
//
// Every GOROOT file gets one fixed modification time: the export data cache is keyed by
// file sizes and times, and the browser's in-memory file system gives back the tar's.
//
//	go run ./deploy/play/bundle -wasm marmot.wasm -o play-fs.tar
//
// Needs node (to run marmot.wasm) and the go command (to list the packages).
package main

import (
	"archive/tar"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/loader"
)

// Packages are the standard packages a playground program may import (with their
// dependencies). Others report that they are not in the playground.
var Packages = strings.Fields(`
	bufio bytes cmp container/heap container/list container/ring context crypto/md5
	crypto/sha256 encoding/base64 encoding/binary encoding/csv encoding/hex encoding/json
	errors fmt hash/crc32 hash/fnv io iter maps math math/big math/bits math/cmplx math/rand
	math/rand/v2 os path reflect regexp runtime slices sort strconv strings sync sync/atomic
	text/tabwriter text/template time unicode unicode/utf16 unicode/utf8 unique`)

// fixedTime is every GOROOT file's modification time in the bundle.
var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	wasm := flag.String("wasm", "", "marmot.wasm (GOOS=wasip1 GOARCH=wasm go build ./cmd/marmot)")
	out := flag.String("o", "play-fs.tar", "output tar")
	root := flag.String("root", ".", "marmot repository (lib/src)")
	keep := flag.Bool("keep", false, "keep the staging directory")
	flag.Parse()
	if *wasm == "" {
		fail(fmt.Errorf("-wasm is required"))
	}
	stage, err := os.MkdirTemp("", "play-bundle-")
	check(err)
	if !*keep {
		defer os.RemoveAll(stage)
	} else {
		fmt.Println("staging:", stage)
	}
	realRoot := goEnv("GOROOT")
	abs, err := filepath.Abs(*root)
	check(err)
	merged, err := loader.MergedGOROOT(realRoot, filepath.Join(abs, "lib", "src"), filepath.Join(stage, "merge"))
	check(err)

	// The packages and their directories, as go list has them for wasip1/wasm.
	cmd := exec.Command("go", append([]string{"list", "-deps", "-f", "{{if .Standard}}{{.ImportPath}}\t{{.Dir}}{{end}}"}, Packages...)...)
	cmd.Env = append(os.Environ(), "GOROOT="+merged, "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0",
		"GOFLAGS=-tags=marmot,purego,noasm,math_big_pure_go", "GOTOOLCHAIN=local", "GOWORK=off")
	cmd.Dir = stage
	listed, err := cmd.Output()
	check(err)
	dst := filepath.Join(stage, "fs")
	var srcBytes int64
	npkg := 0
	for _, line := range strings.Split(strings.TrimSpace(string(listed)), "\n") {
		path, dir, ok := strings.Cut(line, "\t")
		if !ok || path == "unsafe" {
			continue
		}
		npkg++
		to := filepath.Join(dst, "goroot", "src", filepath.FromSlash(path))
		check(os.MkdirAll(to, 0o755))
		ents, err := os.ReadDir(dir)
		check(err)
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue // go/build reads every .go file of the directory; tests are not built
			}
			data, err := os.ReadFile(filepath.Join(dir, n)) // follows the merged tree's links
			check(err)
			check(os.WriteFile(filepath.Join(to, n), data, 0o644))
			srcBytes += int64(len(data))
		}
	}
	// unsafe has no files but must exist for go/build.
	check(os.MkdirAll(filepath.Join(dst, "goroot", "src", "unsafe"), 0o755))
	check(os.WriteFile(filepath.Join(dst, "goroot", "src", "unsafe", "unsafe.go"), mustRead(filepath.Join(merged, "src", "unsafe", "unsafe.go")), 0o644))
	check(filepath.WalkDir(filepath.Join(dst, "goroot"), func(p string, d fs.DirEntry, err error) error {
		check(err)
		return os.Chtimes(p, fixedTime, fixedTime)
	}))
	fmt.Printf("goroot: %d packages, %.1f MB of Go source\n", npkg, float64(srcBytes)/(1<<20))

	// Warm the export data cache with marmot.wasm itself, at the paths the browser uses.
	for _, d := range []string{"root/build/cache", "tmp", "play"} {
		check(os.MkdirAll(filepath.Join(dst, d), 0o755))
	}
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n")
	for _, p := range Packages {
		fmt.Fprintf(&b, "\t_ %q\n", p)
	}
	b.WriteString(")\n\nfunc main() {}\n")
	check(os.WriteFile(filepath.Join(dst, "play", "main.go"), []byte(b.String()), 0o644))
	run := exec.Command("node", "--no-warnings", filepath.Join(abs, "tests", "wasm", "run.mjs"), "--dir", dst+":/",
		*wasm, "play", "-json", "-mode", "dev", "-stages", "ssa,mlir", "/play/main.go")
	run.Env = []string{"HOME=/tmp", "PWD=/play", "TMPDIR=/tmp", "MARMOT_ROOT=/root", "MARMOT_MERGED_GOROOT=/goroot", "PATH=" + os.Getenv("PATH")}
	t0 := time.Now()
	res, err := run.CombinedOutput()
	if err != nil || !strings.Contains(string(res), `"ok": true`) {
		fail(fmt.Errorf("warming the cache with marmot.wasm: %v\n%s", err, lastLines(string(res), 30)))
	}
	fmt.Printf("export data made by marmot.wasm in %.1f s\n", time.Since(t0).Seconds())
	check(os.RemoveAll(filepath.Join(dst, "tmp")))
	check(os.RemoveAll(filepath.Join(dst, "play")))

	// The tar: directories and files, GOROOT with the fixed time, sorted for a stable file.
	f, err := os.Create(*out)
	check(err)
	tw := tar.NewWriter(f)
	var paths []string
	check(filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		check(err)
		if p != dst {
			paths = append(paths, p)
		}
		return nil
	}))
	sort.Strings(paths)
	for _, p := range paths {
		st, err := os.Lstat(p)
		check(err)
		rel := filepath.ToSlash(strings.TrimPrefix(p, dst+string(filepath.Separator)))
		h := &tar.Header{Name: rel, ModTime: st.ModTime(), Mode: 0o644, Format: tar.FormatUSTAR}
		if st.IsDir() {
			h.Typeflag, h.Name, h.Mode = tar.TypeDir, rel+"/", 0o755
		} else {
			h.Typeflag, h.Size = tar.TypeReg, st.Size()
		}
		if len(h.Name) > 100 {
			h.Format = tar.FormatGNU // long names: the browser reads GNU 'L' entries
		}
		check(tw.WriteHeader(h))
		if !st.IsDir() {
			r, err := os.Open(p)
			check(err)
			_, err = io.Copy(tw, r)
			check(err)
			_ = r.Close() // read only
		}
	}
	check(tw.Close())
	check(f.Close())
	st, _ := os.Stat(*out)
	fmt.Printf("%s: %.1f MB\n", *out, float64(st.Size())/(1<<20))
}

func goEnv(k string) string {
	out, err := exec.Command("go", "env", k).Output()
	check(err)
	return strings.TrimSpace(string(out))
}

func mustRead(p string) []byte {
	b, err := os.ReadFile(p)
	check(err)
	return b
}

func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}

func check(err error) {
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "bundle:", err)
	os.Exit(1)
}

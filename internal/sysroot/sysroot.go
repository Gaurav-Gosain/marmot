// Package sysroot fetches the C headers of a target system into marmot's
// cache, so that cgo packages cross compile with nothing else installed:
// marmot sysroot add linux/arm64.
//
// A Linux sysroot is the glibc and kernel headers of Debian's stable
// release, with libc's shared objects and gcc's runtime files: the
// packages are looked up in Debian's package index, downloaded over HTTPS,
// checked against the index's SHA256 and unpacked. That compiles C for the
// target (cgo's type probes and the package's C files) and links the
// executable on this machine with ld.lld.
package sysroot

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Release is the Debian release the headers come from.
const Release = "bookworm"

var mirror = "https://deb.debian.org/debian"

// debArch is Debian's name of a GOOS/GOARCH.
var debArch = map[string]string{"linux/amd64": "amd64", "linux/arm64": "arm64"}

// packages are what a Linux sysroot holds: the C library's headers and
// link files, the kernel headers, and gcc's runtime files (crtbegin.o,
// libgcc) that a link for the target needs.
var packages = []string{"libc6", "libc6-dev", "linux-libc-dev", "libgcc-12-dev", "libgcc-s1"}

// Manifest records what a sysroot was made from.
type Manifest struct {
	Target   string    `json:"target"`
	Source   string    `json:"source"`
	Packages []Package `json:"packages"`
	Added    time.Time `json:"added"`
}

// Package is one unpacked package.
type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// Dir is where the sysroot of goos/goarch lives under root (marmot's
// root: build/cache/sysroot/linux-arm64).
func Dir(root, goos, goarch string) string {
	return filepath.Join(root, "build", "cache", "sysroot", goos+"-"+goarch)
}

// Installed reports whether the sysroot of goos/goarch is there.
func Installed(root, goos, goarch string) bool {
	_, err := os.Stat(filepath.Join(Dir(root, goos, goarch), "manifest.json"))
	return err == nil
}

// Supported lists the targets Add can fetch.
func Supported() []string {
	var ts []string
	for t := range debArch {
		ts = append(ts, t)
	}
	sort.Strings(ts)
	return ts
}

// Add fetches the sysroot of goos/goarch. log reports progress.
func Add(root, goos, goarch string, log func(string)) (*Manifest, error) {
	target := goos + "/" + goarch
	arch, ok := debArch[target]
	if !ok {
		return nil, fmt.Errorf("no sysroot for %s (supported: %s)", target, strings.Join(Supported(), ", "))
	}
	dir := Dir(root, goos, goarch)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), "tmp-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // the temporary directory of an unfinished add
	index := fmt.Sprintf("%s/dists/%s/main/binary-%s/Packages.gz", mirror, Release, arch)
	log("reading " + index)
	entries, err := readIndex(index, packages)
	if err != nil {
		return nil, err
	}
	m := &Manifest{Target: target, Source: "Debian " + Release + " " + mirror, Added: time.Now().UTC()}
	for _, name := range packages {
		e, ok := entries[name]
		if !ok {
			return nil, fmt.Errorf("%s is not in %s", name, index)
		}
		url := mirror + "/" + e["Filename"]
		log(fmt.Sprintf("fetching %s %s", name, e["Version"]))
		deb, err := fetch(url)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(deb)
		if got := hex.EncodeToString(sum[:]); got != e["SHA256"] {
			return nil, fmt.Errorf("%s: SHA256 %s, the index says %s", url, got, e["SHA256"])
		}
		if err := unpackDeb(deb, tmp); err != nil {
			return nil, fmt.Errorf("%s: %v", url, err)
		}
		m.Packages = append(m.Packages, Package{Name: name, Version: e["Version"], SHA256: e["SHA256"]})
	}
	// Absolute symlinks (/lib64/ld-linux-x86-64.so.2 -> /lib/...) point
	// into the sysroot, not into this machine's /.
	if err := relinkAbsolute(tmp); err != nil {
		return nil, err
	}
	// Docs and man pages are not needed. usr/lib (crt files, libc's linker
	// scripts) stays: it makes linking for the target possible too.
	for _, d := range []string{"usr/share"} {
		_ = os.RemoveAll(filepath.Join(tmp, d)) // documentation only
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(dir) // an older sysroot of the target
	if err := os.Rename(tmp, dir); err != nil {
		return nil, err
	}
	return m, nil
}

// Remove deletes the sysroot of goos/goarch.
func Remove(root, goos, goarch string) error {
	dir := Dir(root, goos, goarch)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("no sysroot for %s/%s", goos, goarch)
	}
	return os.RemoveAll(dir)
}

// ReadManifest reads the manifest of an installed sysroot.
func ReadManifest(root, goos, goarch string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(Dir(root, goos, goarch), "manifest.json"))
	if err != nil {
		return nil, err
	}
	m := &Manifest{}
	return m, json.Unmarshal(data, m)
}

var client = &http.Client{Timeout: 5 * time.Minute}

func fetch(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() // read-only response body
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// readIndex returns the index fields of the wanted packages.
func readIndex(url string, want []string) (map[string]map[string]string, error) {
	data, err := fetch(url)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", url, err)
	}
	return parseIndex(zr, want)
}

// parseIndex reads a Debian Packages file (stanzas of "Key: value").
func parseIndex(r io.Reader, want []string) (map[string]map[string]string, error) {
	wanted := map[string]bool{}
	for _, w := range want {
		wanted[w] = true
	}
	out := map[string]map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	cur := map[string]string{}
	flush := func() {
		if n := cur["Package"]; wanted[n] && out[n] == nil {
			out[n] = cur
		}
		cur = map[string]string{}
	}
	for sc.Scan() {
		l := sc.Text()
		if l == "" {
			flush()
			continue
		}
		if l[0] == ' ' {
			continue // a continuation line
		}
		k, v, ok := strings.Cut(l, ": ")
		if ok {
			cur[k] = v
		}
	}
	flush()
	return out, sc.Err()
}

// unpackDeb extracts the data archive of a .deb (an ar archive) into dir.
// The data archive is compressed (xz, zstd or gzip); the system tar reads
// it.
func unpackDeb(deb []byte, dir string) error {
	if !bytes.HasPrefix(deb, []byte("!<arch>\n")) {
		return errors.New("not a .deb (ar) file")
	}
	rest := deb[8:]
	for len(rest) >= 60 {
		hdr := rest[:60]
		name := strings.TrimRight(strings.TrimSpace(string(hdr[:16])), "/")
		var size int
		if _, err := fmt.Sscan(strings.TrimSpace(string(hdr[48:58])), &size); err != nil {
			return fmt.Errorf("bad ar header: %v", err)
		}
		body := rest[60:]
		if size > len(body) {
			return errors.New("truncated .deb")
		}
		if strings.HasPrefix(name, "data.tar") {
			cmd := exec.Command("tar", "-xf", "-", "-C", dir)
			cmd.Stdin = bytes.NewReader(body[:size])
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("tar: %v: %s", err, out)
			}
			return nil
		}
		next := size + size%2 // members are 2-byte aligned
		if next > len(body) {
			break
		}
		rest = body[next:]
	}
	return errors.New("no data archive in the .deb")
}

// GCCDir is gcc's directory in a sysroot (crtbegin.o, libgcc.a), as clang's
// --gcc-install-dir wants it, or "".
func GCCDir(dir string) string {
	ms, _ := filepath.Glob(filepath.Join(dir, "usr", "lib", "gcc", "*-linux-gnu", "*"))
	sort.Strings(ms)
	if len(ms) == 0 {
		return ""
	}
	return ms[len(ms)-1]
}

// relinkAbsolute rewrites the absolute symlinks under dir as relative links
// to the same path inside dir.
func relinkAbsolute(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return err
		}
		target, err := os.Readlink(path)
		if err != nil || !filepath.IsAbs(target) {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(path), filepath.Join(dir, target))
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		return os.Symlink(rel, path)
	})
}

package sysroot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIndex(t *testing.T) {
	idx := `Package: libc6
Version: 2.36
Filename: pool/libc6.deb

Package: libc6-dev
Version: 2.36-9
Description: headers
 continued line
Filename: pool/main/g/glibc/libc6-dev_2.36-9_arm64.deb
SHA256: abc

Package: linux-libc-dev
Version: 6.1
Filename: pool/l.deb
SHA256: def
`
	got, err := parseIndex(strings.NewReader(idx), []string{"libc6-dev", "linux-libc-dev"})
	if err != nil {
		t.Fatal(err)
	}
	if got["libc6-dev"]["Filename"] != "pool/main/g/glibc/libc6-dev_2.36-9_arm64.deb" || got["libc6-dev"]["SHA256"] != "abc" ||
		got["linux-libc-dev"]["Version"] != "6.1" || got["libc6"] != nil {
		t.Errorf("got %v", got)
	}
}

// TestUnpackDeb builds a .deb (an ar archive with a data.tar.gz) and
// unpacks it.
func TestUnpackDeb(t *testing.T) {
	var tb bytes.Buffer
	zw := gzip.NewWriter(&tb)
	tw := tar.NewWriter(zw)
	body := []byte("int x;\n")
	tw.WriteHeader(&tar.Header{Name: "./usr/include/x.h", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	zw.Close()
	var deb bytes.Buffer
	deb.WriteString("!<arch>\n")
	member := func(name string, data []byte) {
		fmt.Fprintf(&deb, "%-16s%-12s%-6s%-6s%-8s%-10d`\n", name, "0", "0", "0", "100644", len(data))
		deb.Write(data)
		if len(data)%2 == 1 {
			deb.WriteByte('\n')
		}
	}
	member("debian-binary", []byte("2.0\n"))
	member("control.tar.gz", []byte("x"))
	member("data.tar.gz", tb.Bytes())
	dir := t.TempDir()
	if err := unpackDeb(deb.Bytes(), dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "usr", "include", "x.h"))
	if err != nil || string(got) != string(body) {
		t.Errorf("x.h: %q %v", got, err)
	}
	if err := unpackDeb([]byte("nope"), dir); err == nil {
		t.Errorf("a non-deb unpacked")
	}
}

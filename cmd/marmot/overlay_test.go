package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadOverlay(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.go")
	if err := os.WriteFile(src, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "pkg", "zz_test.go")
	ov := filepath.Join(dir, "ov.json")
	if err := os.WriteFile(ov, []byte(`{"Replace": {"`+target+`": "`+src+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := readOverlay(ov)
	if err != nil {
		t.Fatal(err)
	}
	if string(m[target]) != "package p\n" || len(m) != 1 {
		t.Errorf("overlay = %q", m)
	}
	if err := os.WriteFile(ov, []byte(`{"Replace": {"`+target+`": ""}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readOverlay(ov); err == nil {
		t.Error("deleting a file: no error")
	}
}

package main

import (
	"io"
	"os"
)

// freshCopy replaces bin with a copy in a new file (a new inode), flushed and, where the
// system allows, dropped from the page cache, so both sides run binaries in the same state.
// The binary as the linker left it measured more RSS (tuios dev --version: 87 to 93 MB as
// built, 68 to 72 MB for a fresh copy), so RSS of perfcmp runs before this copy is suspect.
func freshCopy(bin string) error {
	src, err := os.Open(bin)
	if err != nil {
		return err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return err
	}
	tmp := bin + ".fresh"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		return err
	}
	dropCache(dst)
	if err := dst.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, bin)
}

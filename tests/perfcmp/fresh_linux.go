package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// dropCache asks the kernel to drop f's pages from the page cache (f is flushed).
func dropCache(f *os.File) { _ = unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED) }

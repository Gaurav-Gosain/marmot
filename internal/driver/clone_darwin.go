package driver

import "golang.org/x/sys/unix"

// cloneFile makes dst a copy-on-write clone of src (APFS): no data is
// written, and later writes to either file leave the other as it was.
func cloneFile(src, dst string) error { return unix.Clonefile(src, dst, 0) }

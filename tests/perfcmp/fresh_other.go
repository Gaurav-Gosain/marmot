//go:build !linux

package main

import "os"

// dropCache: no portable way to drop a file's cached pages here; the new inode is the fix.
func dropCache(*os.File) {}

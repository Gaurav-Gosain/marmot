//go:build !unix

package rootdir

import "os"

// lockFile: no file locks here (wasip1: one process, the browser playground's front end).
func lockFile(*os.File, bool) bool { return true }

func unlockFile(*os.File) {}

//go:build !unix

package driver

import (
	"os"
	"syscall"
)

// Without processes or file locks (wasip1: the browser playground's front end, which only
// type-checks and emits SSA and MLIR): no groups, every lock is free.

func groupAttr() *syscall.SysProcAttr   { return nil }
func sessionAttr() *syscall.SysProcAttr { return nil }
func killPG(int)                        {}
func tryLock(*os.File) bool             { return true }

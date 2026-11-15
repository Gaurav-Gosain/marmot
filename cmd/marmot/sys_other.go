//go:build !unix

package main

import "syscall"

// Without processes or statfs (wasip1: the browser playground's front end).

func diskFree(string) (uint64, bool)  { return 0, false }
func groupAttr() *syscall.SysProcAttr { return nil }
func signalGroup(int, syscall.Signal) {}

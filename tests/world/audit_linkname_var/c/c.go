// Package c is imported by b and a. It declares helper without a body: b
// pushes its implementation here with go:linkname.
package c

import _ "unsafe"

var Trace []string

func Note(s string) int { Trace = append(Trace, "c:"+s); return len(Trace) }

// Initialized through a function that reads a later variable.
var First = second() + 1
var Second = Note("Second")

func second() int { return Second * 10 }

func init() { Note("init") }

// Implemented in package b (push).
func Helper(x int) int

// Read by package a through a pull linkname, never referenced here.
var hidden = Note("hidden") * 1000

func hiddenFunc() string { return "hiddenFunc" }

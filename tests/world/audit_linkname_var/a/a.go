package a

import (
	_ "unsafe"

	"marmotworld/audit_linkname_var/b"
	"marmotworld/audit_linkname_var/c"
)

//go:linkname hidden marmotworld/audit_linkname_var/c.hidden
var hidden int

//go:linkname hiddenFunc marmotworld/audit_linkname_var/c.hiddenFunc
func hiddenFunc() string

// Cross-package dependency through a function body.
var A = useLater()

func useLater() int { return Later + c.Helper(3) }

var Later = c.Note("a.Later") + b.B

func init() { c.Note("a.init1") }
func init() { c.Note("a.init2") }

func Hidden() (int, string) { return hidden, hiddenFunc() }

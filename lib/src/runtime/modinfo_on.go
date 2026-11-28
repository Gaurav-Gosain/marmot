//go:build marmot_buildinfo

package runtime

import "unsafe"

// Builds with build info (the default; the driver adds the marmot_buildinfo tag):
// the build info comes from the object the driver links
// (internal/driver/buildinfo.go), read through rt_modinfo; the weak
// defaults in runtime/core/src/modinfo.S give "" without that object.

//go:linkname rt_modinfo rt_modinfo
//go:noescape
func rt_modinfo(p **byte) int

// modinfoText is the build info that debug_modinfo (linkname.go) returns.
var modinfoText string

func init() {
	var p *byte
	if n := rt_modinfo(&p); n > 0 && p != nil {
		modinfoText = unsafe.String(p, int(n))
	}
}

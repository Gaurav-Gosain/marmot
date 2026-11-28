//go:build !marmot_buildinfo

package runtime

// modinfoText is the build info that debug_modinfo (linkname.go) returns:
// none with MARMOT_BUILDINFO=0. A constant, so the runtime compiles as
// it did before build info existed (modinfo_on.go has the other side).
const modinfoText = ""

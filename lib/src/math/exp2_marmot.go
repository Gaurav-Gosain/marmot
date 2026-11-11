// Copied from GOROOT math/exp2_noasm.go without its build constraint: marmot
// always uses the pure Go versions.

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package math

const haveArchExp2 = false

func archExp2(x float64) float64 {
	panic("not implemented")
}

//go:build !arm64

// Copied from GOROOT math/exp_noasm.go without its build constraint: marmot
// uses the pure Go version except on arm64, where exp_marmot_arm64.go ports gc's
// assembly so that results are bit identical to gc.

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package math

const haveArchExp = false

func archExp(x float64) float64 {
	panic("not implemented")
}

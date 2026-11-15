// Copyright 2026 The marmot Authors.

//go:build marmotexplore

package sync

// runtime_exploreYield is a decision point of schedule exploration
// (runtime/explore.go, marmot test -tags marmotexplore with MARMOT_EXPLORE
// set): inside an explored testing/synctest bubble, the member holding the
// baton may hand it to another member here. Outside one it returns at once.
// Provided by the runtime.
func runtime_exploreYield()

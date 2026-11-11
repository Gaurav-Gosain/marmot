//go:build marmot

package main

// startProfile: no CPU profiles when marmot builds itself.
func startProfile() (stop func()) { return func() {} }

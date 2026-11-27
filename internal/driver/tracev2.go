package driver

import (
	"fmt"
	"os"
	"slices"
)

// The execution tracer (runtime/trace) is a build-time switch: with
// MARMOT_TRACE_V2=1 in the environment of marmot build, the runtime package
// gets the marmot_tracev2 tag (lib/src/runtime/trace_marmot.go) and the link
// takes the trace build of the runtime core, build/lib/tracev2 (RT_TRACE=1:
// the scheduler's trace hooks and tracev2.c; make -C runtime/core tracev2).
// Without it the build is the build of before the tracer: profile.go with the
// old stubs (profile_tracev2.go is its trace twin), and a runtime core with no hooks. (MARMOT_* variables are in the cache key.)

const traceV2Tag = "marmot_tracev2"

// traceV2 reports whether the build links the execution tracer.
func (b *build) traceV2() bool { return os.Getenv("MARMOT_TRACE_V2") == "1" }

// checkTraceV2 rejects MARMOT_TRACE_V2=1 where there is no trace build.
func (b *build) checkTraceV2() error {
	if !b.traceV2() {
		return nil
	}
	if b.cfg.Race || b.cfg.Static || b.cross() || b.goarch == "wasm" {
		return fmt.Errorf("MARMOT_TRACE_V2=1: only native builds without -race and -static for now (not %s/%s)", b.goos, b.goarch)
	}
	if lib := b.rtLib("librtcore.a"); !fileExists(lib) {
		return fmt.Errorf("MARMOT_TRACE_V2=1: %s is missing; run make -C runtime/core tracev2", lib)
	}
	return nil
}

// traceV2Tags adds the tracer's tag to tags under MARMOT_TRACE_V2=1.
func (b *build) traceV2Tags(tags []string) []string {
	if !b.traceV2() || slices.Contains(tags, traceV2Tag) {
		return tags
	}
	return append(slices.Clip(tags), traceV2Tag)
}

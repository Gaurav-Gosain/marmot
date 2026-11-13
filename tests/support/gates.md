# Opt-in lifetime and memory transforms

Input of tests/support for docs/support-matrix.md: every transform behind a
switch and whether it is on by default. A transform is on by default only
after a proof review and a full conformance matrix plus fuzz round with it
forced on. Update a row when the switch or the default changes.

| Transform | Switch | Default |
| --- | --- | --- |
| In-place reuse (reuse.go) | MARMOT_REUSE | off |
| Capture by value (escape.go allocSettled) | MARMOT_BYVALUE | off |
| Region inference (region.go) | MARMOT_REGIONS | off |
| Frame regions (frameregion.go) | MARMOT_FRAME_REGIONS | off |
| Allocation sinking (allocsink.go) | MARMOT_ALLOCSINK | off |
| Escape summaries, cross package, extended rules | MARMOT_ESCAPE_SUMMARY, MARMOT_XPKG_ESCAPE, MARMOT_ESCAPE_EXT | off |
| Conversion and make frame buffers (stackconv.go) | MARMOT_STACKCONV | off |
| Intraprocedural escape (default path) | on | on |
| go-strings S3 no-copy (backend) | -no-strings-nocopy | on |
| Defer record elision (panics.go) | MARMOT_DEFERELIDE | off |
| Dead initializer removal (staticinit.go) | MARMOT_DEADINIT | off |
| Allocation without zeroing (rtcore rt_alloc_nozero, 569d095; poison fill c8e0a15) | callers only | on |

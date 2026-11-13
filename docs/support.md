# Support

## Platforms

| Platform | State |
| --- | --- |
| macOS on Apple Silicon | Main development platform. |
| Linux on x86-64 | Works, tested with the same suites. |
| Linux on arm64 | Builds. Not yet run under test. |
| Windows | Not supported. |

## Language

The whole language is supported, including generics, closures, `defer`, `panic` and `recover`,
goroutines, channels, `select`, range over functions, and `unsafe`.

cgo works: packages that `import "C"` build unchanged, with calls both ways (`//export`,
also from C threads), the package's C and assembly files, `#cgo` flags and `pkg-config`, and
gc's pointer passing check. A call to C is a direct native call. Cross compiling cgo packages
needs the target's C headers (`MARMOT_SYSROOT`). See [cgo](usage.md#cgo).

Not supported: plugins and the race detector.

## Standard library

marmot compiles Go's own standard library source. Most packages work. Packages checked by the
test suite include:

`bufio`, `bytes`, `context`, `crypto/sha256`, `encoding/base64`, `encoding/hex`,
`encoding/json`, `errors`, `flag`, `fmt`, `io`, `log`, `log/slog`, `maps`, `math`, `math/big`,
`net`, `net/http`, `os`, `os/exec`, `path/filepath`, `reflect` (most of it), `regexp`, `slices`,
`sort`, `strconv`, `strings`, `sync`, `sync/atomic`, `testing`, `text/template`, `time`,
`unicode/utf8`.

Known gaps: `reflect.MakeFunc`, and parts of `runtime` that expose Go's own scheduler
internals. CPU profiles from `runtime/pprof` work (see [marmot prof](usage.md#tests-benchmarks-and-profiles)).

## Test results

Every test builds a program with both compilers and compares output and exit status.

| Suite | Passing | Total |
| --- | --- | --- |
| Core language | 134 | 135 |
| Garbage collection | 13 | 13 |
| Goroutines and channels | 15 | 15 |
| Standard library | 249 | 249 |
| Memory safety | 50 | 50 |
| Struct layout | 49 | 49 |
| Small applications | 12 | 12 |
| Programs from Go's own test suite | 106 | 108 |
| Real open source modules | 34 | 34 |
| All suites | 729 | 735 |

The full list, feature by feature and package by package, is in
[support-matrix.md](support-matrix.md).

## Known differences from Go

These are cases where the Go specification allows more than one behavior and marmot does not
yet match what the standard Go compiler does.

| Area | Difference |
| --- | --- |
| Evaluation order | In a few statement shapes, a variable is read before a function call in the same statement, where Go reads it after. |
| NaN values | The exact bit pattern of a NaN result can differ. It is still a NaN. |
| Floating point | `math.Log` can differ in the last bit for some inputs on arm64. |
| Stack traces | Panic messages match Go. The stack trace that follows has a different layout. |
| String data | In `-release` builds, equal strings built at compile time may share their bytes. Only visible through `unsafe`. |

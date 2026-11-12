# marmot

A Go compiler built on MLIR and LLVM. Keep your Go code, switch the compiler, get small native
executables.

    marmot build ./cmd/app        # dev mode: per package cache, fast rebuilds
    marmot build -release ./cmd/app   # whole program optimization
    marmot run main.go
    marmot test ./...

Status: experimental. It compiles real Go programs, including itself, but it is one day old and
not ready for production use. Read "What does not work yet" before trying it.

## How it works

Go source is type checked with `go/types`, lowered to SSA with `golang.org/x/tools/go/ssa`, and
emitted as MLIR in two dialects: upstream `llvm`, and a custom `go` dialect that keeps Go
semantics (allocation, bounds checks, interface calls, defers, strings, maps, channels) visible
to optimization passes. A C++ backend runs those passes, lowers to LLVM, and emits objects in
process. The runtime (garbage collector, M:N scheduler, channels, maps) is a small C core plus a
Go `runtime` package, linked into every binary.

See [SPEC.md](SPEC.md) for the design and [docs/SUPPORT.md](docs/SUPPORT.md) for what is
supported.

## Where it stands

Numbers are preliminary. They come from `bench/` on an Apple M3 Pro with default flags, with
outputs checked equal to Go for every row. The Go baselines still need a rerun on a quiet
machine, so treat these as working numbers, not a published benchmark.

| | marmot | go |
| --- | --- | --- |
| hello world binary | 148 KB (100 KB at `-Oz`) | 1.68 MB |
| run time, 43 programs, geometric mean | 1.6x faster in `-release`, 1.4x in dev | 1.0 |
| programs slower than go | 10 of 43 | |
| rebuild, nothing changed | about equal | |
| rebuild of a large program after a one line edit | about 4x slower | |

Loop transforms that vectorize and parallelize numeric loops reach 12x to 36x on programs such
as matrix multiply and mandelbrot. They are opt-in while they go through review, so they are not
in the table above.

Conformance: 134 of 134 core language tests, and most of the standard library and real world
module tiers; current counts are in [docs/SUPPORT.md](docs/SUPPORT.md). Targets: darwin/arm64 and
linux/amd64.

## How it is checked

Speed work on a compiler is only worth something if the output is right. Three things guard
that here:

- A conformance suite that compares every program with stock Go, plus a differential fuzzer.
- Independent adversarial audits of every optimization that is on by default
  ([docs/audit-frontend.md](docs/audit-frontend.md), [docs/audit-backend.md](docs/audit-backend.md)).
- Machine checked proofs of optimizer rules in Bend 2 and Lean 4, and checkers that re-verify
  real compiler output ([verify/README.md](verify/README.md), [docs/verify.md](docs/verify.md)).

In the first day these found more than twenty miscompiles that the test suite had passed,
several of them memory safety bugs. Every optimization that failed review is off by default
until it is fixed, signed off, and passes the full matrix with it forced on (SPEC rule 11).

## What does not work yet

- cgo, plugins, the race detector, Windows.
- Parts of `reflect` and some standard library packages; see docs/SUPPORT.md.
- Build time on large programs after an edit is several times slower than `go build`.
- A few behaviors differ from the gc compiler where the Go spec leaves them open (evaluation
  order in some statements, NaN payloads); they are listed in docs/SUPPORT.md.

## Building

Needs Go 1.27, LLVM and MLIR 22, CMake and Ninja.

    make            # frontend, backend, runtime
    make test

The name: a marmot is a close relative of the gopher, and it starts with M, like MLIR.

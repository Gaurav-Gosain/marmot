# marmot

A Go compiler built on MLIR and LLVM. Keep your Go code, switch the compiler, and get small
native executables.

```sh
marmot build ./cmd/app             # fast rebuilds
marmot build -release ./cmd/app    # whole program optimization
marmot run main.go
marmot test ./...
```

It also has the tools people install next to `go`: `marmot fix`, `fmt`, `check`, `watch`, `repl`,
`bench`, `prof`, `size`, `generate`, `doctor`, and `mcp` for coding agents. Errors come with the
source, the exact span and a suggested fix, or as JSON. See [Usage](docs/usage.md).

Status: experimental. marmot compiles real Go programs, including itself, but it is new and not
ready for production use.

## At a glance

Measured against `go build` on an Apple M3 Pro. Every program's output is checked against Go.
Details and method in [docs/benchmarks.md](docs/benchmarks.md).

| Binaries | marmot | go |
| --- | --- | --- |
| hello world with `println`, `-release` | 148 KB | 1.68 MB |
| hello world with `fmt.Println`, `-release` | 665 KB | 2.4 MB |
| run time, average over 43 programs (`-release`) | 1.6x faster | 1.0x |

| Build time, the compiler itself (238 packages) | marmot | go |
| --- | --- | --- |
| first build, empty caches | 5.1 to 5.4 s | 5.2 to 6.3 s |
| rebuild, nothing changed | 0.05 to 0.07 s | 0.14 to 0.15 s |
| rebuild after a one line edit | 0.68 to 0.96 s | 0.50 to 0.93 s |

Dev builds use a fast code generator by default (`MARMOT_TIER0=0` turns it off). After a dev build returns, the packages it
compiled are compiled again with LLVM at low priority, and later builds link the optimized
objects (`MARMOT_TIER1=0` turns it off). Rebuilds use
the incremental front end, which compiles only changed packages and their importers
(`MARMOT_INCREMENTAL=0` turns it off).

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/img/overview-dark.svg">
  <img alt="How marmot turns Go source into a native binary" src="docs/img/overview-light.svg">
</picture>

## Documentation

- [Usage](docs/usage.md): commands, flags, build modes.
- [Design](docs/design.md): how the compiler and runtime are put together.
- [Benchmarks](docs/benchmarks.md): speed, size and build time, with the method.
- [Support](docs/support.md): what works, what does not, and known differences from Go.

## Building from source

Needs Go 1.27, LLVM and MLIR 22, CMake and Ninja.

```sh
make
make test
```

The name: a marmot is a close relative of the gopher, and it starts with M, like MLIR.

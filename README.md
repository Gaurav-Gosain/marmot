# marmot

A Go compiler built on MLIR and LLVM. Keep your Go code, switch the compiler, and get small
native executables.

```sh
marmot build ./cmd/app             # fast rebuilds
marmot build -release ./cmd/app    # whole program optimization
marmot run main.go
marmot test ./...
```

Status: experimental. marmot compiles real Go programs, including itself, but it is new and not
ready for production use.

## At a glance

Measured against `go build` on an Apple M3 Pro. Every program's output is checked against Go.
Details and method in [docs/benchmarks.md](docs/benchmarks.md).

| Binaries | marmot | go |
| --- | --- | --- |
| hello world with `println`, `-release` | 148 KB | 1.68 MB |
| hello world with `fmt.Println`, `-release` | 1.1 MB | 2.4 MB |
| run time, average over 43 programs (`-release`) | 1.6x faster | 1.0x |

| Build time, the compiler itself (182 packages) | marmot | go |
| --- | --- | --- |
| first build, empty caches | 3.1 s | 3.9 s |
| rebuild, nothing changed | 0.04 s | 0.12 s |
| rebuild after a one line edit | 0.33 to 0.62 s | 0.32 to 0.75 s |

Dev builds use a fast code generator by default (`MARMOT_TIER0=0` turns it off). Rebuild times
are with the incremental front end, `MARMOT_INCREMENTAL=1`, which becomes the default soon.

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

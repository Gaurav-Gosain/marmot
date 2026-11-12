# Usage

marmot follows the `go` command where it can. Run it inside a Go module.

## Commands

| Command | What it does |
| --- | --- |
| `marmot build [packages]` | Compile packages and their dependencies into an executable. |
| `marmot run file.go` | Compile and run a program. |
| `marmot test [packages]` | Compile and run package tests, like `go test`. |
| `marmot cache` | Show the build cache size. `cache trim` and `cache clean` shrink or remove it. |
| `marmot version` | Print the version. |

## Build modes

There are two modes, because fast rebuilds and deep optimization pull in opposite directions.

| Mode | Command | Use it for |
| --- | --- | --- |
| dev (default) | `marmot build` | Day to day work. Each package is compiled on its own and cached. |
| release | `marmot build -release` | Shipping. The whole program is optimized together. Builds take longer. |

## Common flags

| Flag | Meaning |
| --- | --- |
| `-o file` | Name of the output file. |
| `-release` | Release mode: whole program optimization at `-O3`. |
| `-O level` | Optimization level: `0`, `1`, `2`, `3`, `s` or `z`. `z` gives the smallest binary. |
| `-s` | Leave out file and line tables. Smaller binary, but panics show no source lines. |
| `-target triple` | Build for another platform, for example `linux/amd64`. |
| `-tags a,b` | Build tags. |
| `-pgo-gen`, `-pgo file` | Profile guided optimization: build an instrumented binary, run it, then build again with the profile. |
| `-emit kind` | Stop early and write `mlir`, `llvm` or `obj` instead of an executable. |
| `-v`, `-x` | Print progress and timing, or the commands that run. |

## Examples

Smallest possible binary:

```sh
marmot build -release -O z -s -o app ./cmd/app
```

Build for Linux from a Mac:

```sh
marmot build -target linux/amd64 -o app-linux ./cmd/app
```

Run the tests of the current module:

```sh
marmot test ./...
```

## The build cache

Compiled packages are cached, so a rebuild with no changes does almost no work. The cache is
capped at 4 GB by default and old entries are removed automatically. Set `MARMOT_CACHE_MAX_MB` to
change the cap.

## Environment variables

| Variable | Meaning |
| --- | --- |
| `GOMAXPROCS` | Number of threads that run Go code, as in Go. |
| `GOGC` | Garbage collection target percentage, as in Go. |
| `MARMOT_CACHE_MAX_MB` | Size cap of the build cache in megabytes. |
| `MARMOT_RELEASE=1` | Make `-release` the default. |
| `MARMOT_TIER0=0` | Dev builds use LLVM instead of the fast code generator (the default). Slower builds, faster code. |

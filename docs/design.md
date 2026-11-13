# Design

marmot reuses the parts of the Go toolchain that define the language, and replaces the parts
that generate code.

## Pipeline

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="img/pipeline-dark.svg">
  <img alt="The compiler pipeline from Go source to executable" src="img/pipeline-light.svg">
</picture>

1. **Front end.** The standard `go/types` checker and the `x/tools` SSA builder turn source
   into a simple, typed instruction form. Generic code is expanded here.
2. **MLIR.** Each package becomes an MLIR module. Plain operations use MLIR's `llvm` dialect.
   Operations that carry Go meaning use a small `go` dialect.
3. **Go-aware passes.** These run while the program still says "allocate a slice" or "call
   through an interface", which makes some optimizations much easier than on LLVM IR.
4. **LLVM.** The module is lowered to LLVM IR, optimized, and turned into machine code. All of
   this happens inside one backend process.
5. **Link.** Objects are linked with the runtime by the system linker. Unused code is removed.

## What lowering looks like

One function, captured at each level with `marmot build -emit mlir`, `-emit llvm` and `-emit obj`
(debug locations left out):

```go
func Sum(xs []int32) int32 {
	var s int32
	for _, x := range xs {
		s += x
	}
	return s
}
```

In MLIR the loop still says what Go means. `go.index` is an index into a slice, with its bounds
check:

```mlir
%v9 = llvm.extractvalue %a0[0] : !llvm.struct<(ptr, i64, i64)>
%v10 = llvm.extractvalue %a0[1] : !llvm.struct<(ptr, i64, i64)>
%v11 = go.index %v9[%v7] len %v10, i32 : i64
%v12 = llvm.load %v11 {alignment = 4 : i64, go.unsafe} : !llvm.ptr -> i32
%v13 = llvm.add %v1, %v12 : i32
```

In LLVM IR at `-O 2` the check is gone and the loop works on four `int32` values at a time:

```llvm
%wide.load = load <4 x i32>, ptr %5, align 4
%wide.load12 = load <4 x i32>, ptr %6, align 4
%wide.load13 = load <4 x i32>, ptr %7, align 4
%wide.load14 = load <4 x i32>, ptr %8, align 4
%9 = add <4 x i32> %wide.load, %vec.phi
```

On arm64 that is two 128 bit loads and four vector adds each time round:

```text
ldp q4, q5, [x8, #-0x20]
ldp q6, q7, [x8], #0x40
add.4s v0, v4, v0
add.4s v1, v5, v1
add.4s v2, v6, v2
add.4s v3, v7, v3
```

## Why a Go dialect

LLVM sees memory, branches and calls. It does not know that a branch is a bounds check, or that
a call allocates. The `go` dialect keeps that knowledge long enough to use it.

| Group | Examples | What the passes do with it |
| --- | --- | --- |
| Allocation | `new`, `make`, closures, boxing | Keep objects on the stack when they do not outlive the function. |
| Indexing | index, slice, bounds and nil checks | Remove checks that can be shown never to fail. |
| Interfaces | interface calls, type assertions | Call the method directly when the type is known. |
| Control | `defer`, `panic`, `go` | Make `defer` cheap when no panic can reach it. |
| Strings | concatenation, conversions | Join concatenations, avoid copies when safe. |
| Loops | counted loops over slices | Vectorize, and run independent iterations in parallel. |

## Build modes

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="img/modes-dark.svg">
  <img alt="Dev mode compiles packages separately, release mode compiles the whole program" src="img/modes-light.svg">
</picture>

In dev mode each package is compiled separately and cached, so an edit recompiles little. In
release mode the optimizer sees the whole program, which allows inlining across packages and
removal of code that nothing uses.

## Runtime

Every binary carries a small runtime in two layers.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="img/runtime-dark.svg">
  <img alt="The two layers of the runtime" src="img/runtime-light.svg">
</picture>

| Part | How it works |
| --- | --- |
| Garbage collector | Mark and sweep. It does not move objects. Marking runs on several threads. |
| Goroutines | Each has its own stack. Many goroutines share a few OS threads, and idle threads take work from busy ones. |
| Channels | Unbuffered channels hand values directly to the waiting goroutine. Buffered channels use a lock-free ring. |
| Maps | Open addressing in groups, with fast paths for integer and string keys. |
| Panics | `defer`, `panic` and `recover` follow Go's rules, including stack traces with file and line. |
| Library fast paths | Byte search, UTF-8, hex, base64, CRC and SHA-2 use vector instructions where the CPU has them. |

The standard library is Go's own source. marmot replaces only the packages that are tied to
Go's runtime or written in assembly.

## Keeping it correct

A fast compiler is only useful if its output is right. marmot is checked in three ways.

- **Comparison with Go.** A suite of several hundred programs is built with both compilers,
  and the output and exit status must match. A fuzzer generates more programs at random.
- **Review of every optimization.** Each optimization is switched on by default only after its
  rule is written down and it passes the whole suite with the optimization forced on.
- **Proofs.** The rules behind the riskier optimizations (bounds check removal, loop
  parallelization, object lifetimes) are proved in Bend 2 and Lean 4, and a separate checker
  re-examines what the compiler actually did.

## Repository layout

| Path | Contents |
| --- | --- |
| `cmd/marmot` | The command line tool. |
| `internal/` | Front end: loading, code generation to MLIR, build driver. |
| `backend/` | C++ backend: the `go` dialect, passes, LLVM lowering. |
| `runtime/core/` | C part of the runtime. |
| `lib/src/` | Go part of the runtime and replaced standard library packages. |
| `tests/` | Conformance programs and the tools that run them. |
| `bench/` | Benchmark programs and runner. |
| `verify/` | Proofs and checkers. |

# Usage

marmot follows the `go` command where it can. Run it inside a Go module.

## Commands

| Command | What it does |
| --- | --- |
| `marmot build [packages]` | Compile packages and their dependencies into an executable. |
| `marmot run file.go` | Compile and run a program. |
| `marmot test [packages]` | Compile and run package tests, like `go test`. |
| `marmot fix [packages]` | Apply the machine applicable fixes of build errors, then of check findings (formatting, modernizers). `-code M0004,M0005` limits it to those build error codes; `-all` adds fixes that may be incorrect, such as typo corrections and vet or lint suggestions. `-only fmt,modernize` applies only those check fixes, `-diff` prints them as patches, `-test` keeps a fix only if its package tests pass, `-upgrade go1.N` raises the go version first. |
| `marmot explain CODE` | Describe an error code. With no code, list all of them. |
| `marmot check [packages]` | Run vet, lint, format and the other code checks in one pass (see [Checks](#checks)). |
| `marmot watch [build\|run\|test]` | Build, run or test again after every change to a `.go` file, `go.mod` or `go.sum`. |
| `marmot repl` | Evaluate Go declarations, statements and expressions interactively. |
| `marmot mcp` | Serve build, run, test, fix and explain to coding agents over the Model Context Protocol. |
| `marmot fmt [packages]` | Format Go source and fix its imports, as goimports does. `-l` lists, `-d` shows a diff, `-check` fails when a file is not formatted. |
| `marmot generate [packages]` | Run `//go:generate` directives, restoring cached outputs when nothing they depend on changed. |
| `marmot size BINARY` | Size by section, package and function; `-why FUNC` shows a chain of calls from `main` to a function. |
| `marmot bench [package]` | Run benchmarks several times; `-vs go` compares with `go test`, `-compare FILE` with a saved run. |
| `marmot prof PROFILE` | Read a pprof profile: top functions, call tree (`-tree`), annotated source (`-list`). |
| `marmot doctor` | Check the installation and the environment and say what to fix. |
| `marmot completion SHELL` | Print shell completion for bash, zsh, fish or powershell. |
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
| `-allocs`, `-allocs-all` | Report the allocation sites of the main module (or of every package): heap or stack, and why (see [Allocation report](#allocation-report)). |

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

Apply the safe fixes of the build errors, then see what an error code means:

```sh
marmot fix ./cmd/app
marmot explain M0014
```

Format the module and fix its imports:

```sh
marmot fmt ./...
```

Rebuild and restart a server on every save:

```sh
marmot watch run ./cmd/server -port 8080
```

Try an expression:

```sh
echo 'strings.Fields("a b c")' | marmot repl
```

Compare benchmarks with `go test`, then look at a CPU profile:

```sh
marmot bench -vs go -cpuprofile cpu.out ./pkg
marmot prof -tree cpu.out
```

See what a binary is made of, and why a function is in it:

```sh
marmot size -why fmt.Sprintf ./app
```

Check the installation:

```sh
marmot doctor
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
| `MARMOT_OUTPUT` | Diagnostic output: `json`, `human` or `plain`. See below. |
| `NO_COLOR` | Turn off color in human output. |
| `MARMOT_ALLOC_REPORT=FILE` | Write the code generator's allocation decisions to FILE, one JSON object per line (see [Allocation report](#allocation-report)). |
| `MARMOT_TIER0=0` | Dev builds use LLVM instead of the fast code generator (the default). Slower builds, faster code. |
| `MARMOT_TIER1=0` | Turn off background tier 1: after a dev build returns, the packages it compiled are compiled again with LLVM at -O2 on idle cores, and later builds link those objects. |

## Tests, benchmarks and profiles

On a terminal `marmot test` shows each failure first, at the line of its `t.Error` or `t.Fatal`
with the source, then every test and subtest with its status and time, then a summary. Off a
terminal the output is the same as `go test`. `marmot test -watch` runs the tests again after
every change.

`marmot bench -vs go ./pkg` builds the package's benchmarks with marmot and with `go test`,
runs them alternately (`-count`, default 6), and prints the median and spread of each, the
change and whether it is significant (Mann-Whitney U, as benchstat does). Benchmarks are built
at `-O 2` unless you give a level or `-release`. A benchmark under 1 ns per iteration gets a
note: the optimizer probably removed work whose result was unused. `-cpuprofile FILE` profiles
the last run; read it with `marmot prof FILE`.

`marmot prof` reads any pprof CPU or memory profile: the functions with the most cost (flat
and cumulative), the hottest path, `-tree` for the call tree as a text flame graph, `-list
REGEXP` for the source of matching functions with the cost of each line, `-sample
alloc_space` and friends for memory profiles, `-json` for programs.

Programs built by marmot record CPU profiles with the standard `runtime/pprof` API
(`pprof.StartCPUProfile`), so the same profile can be read with `marmot prof` or `go tool
pprof`. On Linux each thread is sampled on its own CPU clock; on macOS the process timer is used.

## Binary size

`marmot size app` lists the sections of the executable, the code of each package and the
largest functions (names come from marmot's line table, or from the symbol table for other
binaries). `-pkg fmt` lists one package's functions; `-why fmt.Sprintf` prints a chain of direct
calls from `main` to the function, which says why it is linked in.

## Allocation report

`marmot build -allocs` prints, for each allocation in the main module that goes to the heap,
where it is, what it allocates and why it could not stay in the stack frame. `-allocs-all`
does the same for every package, the standard library included. Packages are compiled again
for the report, so the build cache does not hide them.

```text
note[M0111]: heap allocation: composite of `point` (16 bytes) in example.com/ad.newPoint
 --> ./main.go:9:42
  |
9 | func newPoint(x, y int) *point { return &point{x, y} }
  |                                          ^^^^^^^^^^^ returned from the function

note[M0111]: heap allocation: make-slice of `[4]point` (64 bytes) in example.com/ad.main
  --> ./main.go:20:8
   |
20 |     ps := make([]point, 0, 4)
   |           ^^^^^^^^^^^^^^^^^^^ passed to append, which may keep it (line 22)

note[M0111]: heap allocation: box of `int` (8 bytes) in example.com/ad.main
  --> ./main.go:27:23
   |
27 |     fmt.Println(sum(ps), keep.x)
   |                          ^^^^^^ stored in the variadic arguments, which are passed to fmt.Println, which may keep it

6 allocation sites: 5 on the heap, 1 on the stack
```

The reason is the first use that makes the value escape: returned, stored in a global or in
memory that outlives the function, passed to a call that may keep it, captured by a closure,
sent on a channel, converted to an interface, or too large for the frame. A heap allocation
inside a loop gets a note, since it happens once per iteration. Sites the back end may still
move to the stack are marked as such.

The kinds are `new`, `composite`, `var` (a local whose address escapes), `varargs`, `closure`,
`make-slice`, `make-map`, `make-chan`, `box` (a value converted to an interface) and `convert`
(string and byte or rune slice conversions). With `-json` each site is a diagnostic with code
M0111, followed by a summary line `{"kind": "allocs", "sites", "heap", "heapInLoops",
"stack", "region", "static"}`.

`MARMOT_ALLOC_REPORT=FILE` writes the raw decisions of any build, stack sites included, one
JSON object per line:

```json
{"fn":"example.com/ad.newPoint","pos":"/src/ad/main.go:9:47","kind":"composite","type":"point","bytes":16,"where":"heap","reason":"returned from the function (line 9)","final":true}
```

`where` is `stack`, `heap`, `region` (in the caller's region, freed when the caller is done) or
`static`. `final` is false when the back end may still change the decision.

## cgo

Packages that `import "C"` build unchanged: cmd/cgo itself reads the preamble and maps the C
declarations (types, functions, variables, enums, `#define` constants, `C.CString` and the
other helpers, errno results), and `#cgo CFLAGS`, `CPPFLAGS`, `LDFLAGS` and `pkg-config`
lines are honored, with the same flag allowlist as `go build` (`CGO_CFLAGS_ALLOW` and friends
extend it). The package's `.c`, `.s` and `.S` files are compiled with clang and cached, and
`//export` lets C call Go, also from threads that C created.

A call to C is a direct native call on the goroutine's own stack, with no argument copying
and no stack switch. While a C function runs, its goroutine gives up its processor, so a C
call that blocks does not hold up other goroutines. C functions that are known to return
quickly skip that step: functions of the C library such as `strlen`, `memcpy` and the
`math.h` functions, and functions marked with both `#cgo noescape` and `#cgo nocallback`
(marmot reads these two together as "short and does not block"; do not mark a function that
can sleep or wait). `marmot build -release` also compiles the C code to bitcode and
optimizes it together with the Go code, so small C functions inline into their Go callers.

Go pointers passed to C are checked as with gc (`GODEBUG=cgocheck=0` turns the check off):
Go memory passed to C may not hold Go pointers unless they are pinned with `runtime.Pinner`.

Programs without `import "C"` are built exactly as before, and the standard library is always
built without cgo (`net` and `os/user` behave as with `CGO_ENABLED=0`). Cross compiling cgo
packages is not supported yet.

## Cached code generation

`marmot generate ./...` runs `//go:generate` directives like `go generate`, and remembers what
each one wrote, keyed by the directive, its environment, the generator binary and the
package's other files. When none of those changed, the outputs are restored instead of running
the generator. `-n` lists directives and whether they are cached; `-force` runs everything.
`marmot watch -generate` runs it before every build.

## Watch mode

`marmot watch` builds the current package now and again after every change. `marmot watch run
./cmd/server -port 8080` stops the program and starts the new build after each change;
`marmot watch test ./...` runs the tests again. The screen is cleared before each round on a
terminal (`-clear=false` keeps the scrollback); `-interval` sets how often files are checked.
Rebuilds go through the build cache, so only what changed is compiled again.

## The REPL

`marmot repl` reads Go one entry at a time. An expression prints its value, a statement runs, a
`func` or `type` declaration is kept for later entries. Packages of the standard library are
imported when you use them (`strings.ToUpper("a")` just works); `import "example.com/x"` adds
others. Tab or the right arrow accepts the completion shown in grey; history is kept in
`~/.marmot_history`. Commands: `:type EXPR`, `:doc NAME`, `:time`, `:imports`, `:reset`,
`:quit`.

Each entry is compiled and run as a whole program with the entries before it, and only the new
output is shown. Earlier statements run again each time, so their side effects repeat. An entry
that does not compile or panics is not kept.

## Checks

`marmot check` runs the checks a project usually runs one by one in CI, in one process over one
package graph. With no packages it checks every module of the `go.work`, or the current module.

| Check | Reports | Fails the run |
| --- | --- | --- |
| `fmt` | files gofmt or goimports would change | yes |
| `vet` | the analyzers of the installed `go vet` | yes |
| `lint` | the golangci-lint standard set (staticcheck, errcheck, govet, ineffassign, unused) and the gosec file permission rules; reads a v2 `.golangci.yml` | yes |
| `gopls` | what `gopls check` reports | yes |
| `vuln` | vulnerabilities the code calls, as govulncheck finds them | yes |
| `tidy` | a `go.mod` or `go.sum` that `go mod tidy` would change | yes |
| `cross` | type errors on other targets: `-targets`, `scripts/platforms.txt` or `platforms.txt`, else linux, darwin and windows on amd64 and arm64 | yes |
| `modernize` | what the `go fix` modernizers would change | no |
| `exhaustive` | switches that miss values of an enum or types of a sealed interface (a switch with a `default` is left alone); runs only when `-only` names it or `.golangci.yml` enables `exhaustive` | no |
| `nil` | dereferences of pointers and writes to maps that can be nil | no |
| `deadcode` | functions no program or test reaches | no |

`-only` picks checks; `-changed [REF]` checks only the packages whose files changed since REF
(by default `origin/main`, else `main`):

    marmot check -changed -only vet,lint

Results are cached per package, keyed by its files and what its dependencies export, so an
unchanged tree answers at once and an edit reanalyzes only what it affects (`-nocache` turns this
off). On a workspace of 10 modules and 204 packages, the full set takes 46 s from an empty cache
(251 s for the same tools run one by one), 0.7 s when nothing changed and about 1 s after an
edit, and stays under 2 GB of memory.

In JSON mode each finding is a `diagnostic` object with `check` and `analyzer` fields (codes
M0100 to M0110), then one `{"kind":"check","check":"lint","status":"failed","findings":3,...}`
object per check, then the `result` line. The analyzers live in `marmot-check`, installed next
to `marmot` (`make check-tool`).

### Fixes

`marmot fix` applies the machine applicable fixes of build errors, then those of `fmt` and
`modernize`. After each round it type checks the changed packages and undoes the fixes of any
package that no longer builds; `-test` also requires their tests to pass. `-diff` prints the
patches and changes nothing:

    marmot fix -diff -only modernize

## Coding agents

`marmot mcp` is a Model Context Protocol server on stdin and stdout. Its tools `build`, `run`,
`test`, `fix` and `explain` take a directory and return the JSON output described below. To
register it with Claude Code: `claude mcp add marmot -- marmot mcp`.

## Error messages

Every error has a stable code (`M0002`) that `marmot explain` describes. There are three output
modes:

| Mode | When | What it looks like |
| --- | --- | --- |
| human | stderr is a terminal | `error[CODE]: message`, then `--> file:line:col`, the source with carets under the exact span and a label saying what is wrong there, labels on related spans, `= note:` and `= help:` lines, and fixes as `help:` with a diff. At most 10 errors, sorted by file and line, then a summary. |
| plain | stderr is not a terminal | `file:line:col: message` lines, as the go tool prints them. Tools and tests parse this form; it does not change. |
| json | `--json`, `MARMOT_OUTPUT=json`, or a coding agent environment | JSON Lines on stderr, described below. |

`MARMOT_OUTPUT=human`, `plain` or `json` overrides the choice. `--color=auto|always|never` sets
color in human mode; `auto` respects `NO_COLOR` and `TERM=dumb`.

JSON is the default when one of these variables is set (to anything but `0`) and `MARMOT_OUTPUT`
is not: `CLAUDECODE`, `AI_AGENT`, `CODEX_SANDBOX`, `GEMINI_CLI`, `CURSOR_AGENT`, `OPENCODE`,
`GOOSE_TERMINAL`.

### JSON schema, version 1

Each line is one object with `"schema": 1` and a `kind`. A command writes zero or more
`diagnostic` lines and ends with one `result` line.

```json
{"schema":1,"kind":"diagnostic","severity":"error","code":"M0002","message":"undefined: fmt.Printn",
 "span":{"file":"/src/app/main.go","line":15,"col":6,"endLine":15,"endCol":12},
 "help":["a func with a similar name exists: fmt.Println"],
 "fixes":[{"title":"use fmt.Println","edits":[{"file":"/src/app/main.go",
   "span":{"file":"/src/app/main.go","line":15,"col":6,"endLine":15,"endCol":12},"newText":"Println"}]}],
 "context":{"function":"func main()","lines":[{"line":15,"text":"\tfmt.Printn(\"x\")"}]},
 "explain":"marmot explain M0002"}
{"schema":1,"kind":"result","command":"build","ok":false,"errors":1,"warnings":0,"durationMs":310,"exitCode":1}
```

`diagnostic` fields:

| Field | Meaning |
| --- | --- |
| `severity` | `error`, `warning` or `note`. |
| `label` | What is wrong at the primary span, readable on its own. |
| `code` | Stable code, `M0000` when there is no more specific one. |
| `message` | The message, as in plain mode. |
| `span` | Primary location. Lines and columns are 1-based, columns count bytes, `endCol` is exclusive. Absent location: `file` is empty. |
| `labels` | Secondary spans, each `{span, text}`. |
| `notes`, `help` | Lists of strings. |
| `fixes` | Suggested fixes, each `{title, applicability, edits}`; an edit is `{file, span, newText}` and replaces the span (an empty span inserts). `applicability` is `machine-applicable` (safe to apply unseen), `maybe-incorrect` (likely, check it) or `has-placeholders` (never apply as is). Applying every edit of one fix resolves the diagnostic. |
| `related` | Other locations, each `{span, message}`, for example the declaration of a called function. |
| `context` | What you would open the file for: `function` (enclosing signature), `lines` (source around the span, `{line, text}`), `types` (one line definitions of the types the message names). |
| `bundle` | Internal compiler errors only: a directory with the inputs for a bug report. |
| `rendered` | The human form without color, exactly what a person would see. |
| `fullMessage` | Present when `message` was shortened (long type names elided). |
| `explain` | The command that describes the code. |

New fields may be added within schema 1; parsers must ignore fields they do not know.

Other commands use the same envelope (`schema`, `kind`) with their own kinds:

| Command | Kinds |
| --- | --- |
| `marmot test -json` | `test` per test, `diagnostic` per failure, `result` |
| `marmot fix -json` | `fix` per applied fix, then `diagnostic` and `result` |
| `marmot fmt -json` | `format` per changed file (`file`, `written`), `result` |
| `marmot generate -json` | `generate` per directive (`command`, `cached`, `outputs`), `result` |
| `marmot watch -json` | `watch` events (`build`, `done`) around each round's own output |
| `marmot size -json` | one `size` object: `sections`, `packages`, `functions`; `-why` gives `why` with `chain` |
| `marmot bench -json` | `bench` per benchmark and unit, with the summaries, `delta` and `p` |
| `marmot prof -json` | one `profile` object: `functions`, `lines`, and `tree` with `-tree` |
| `marmot doctor -json` | `doctor` per check (`check`, `status`, `detail`, `fix`), `result` |

`result` fields: `command`, `ok`, `errors`, `warnings`, `output` (the file written, on success),
`durationMs`, `exitCode`.

In json mode all errors are listed (no cap), duplicates removed, in a stable order. `marmot fix
--json` writes one `{"kind":"fix","file","code","title","line"}` line per applied fix, then the
remaining diagnostics and the result. `marmot explain --json CODE` writes `{code, title, text,
wrong, right, fix}` to stdout: the description, code with the mistake, the same code fixed, and
the usual fix.

`marmot test --json` writes one `{"kind":"test", "package", "name", "status", "durationMs",
"file", "line", "message", "output"}` line per test and subtest, a `diagnostic` (code `M0022`)
per failure with its source context, and the result.

### Runtime errors

A program built by marmot reports a panic or fatal error in the same three modes, chosen when
it happens: `MARMOT_OUTPUT=json` writes one object, `{"schema":1, "kind":"panic", "code":"M0020",
"message", "valueType", "chain", "goroutine", "frames":[{"func", "file", "line", "col",
"inlined", "user"}], "goroutines":{"total", "running", "runnable", "waiting"}, "exitCode":2}`
(`kind` `fatal` and code `M0021` for fatal errors). On a terminal the report lists the program's
frames with their source lines and folds library frames. Otherwise the trace is the plain Go
form. `marmot run` in json mode passes `MARMOT_OUTPUT=json` to the program.

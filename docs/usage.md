# Usage

marmot follows the `go` command where it can. Run it inside a Go module.

## Commands

| Command | What it does |
| --- | --- |
| `marmot build [packages]` | Compile packages and their dependencies into an executable. |
| `marmot run file.go` | Compile and run a program. |
| `marmot test [packages]` | Compile and run package tests, like `go test`. |
| `marmot fix [packages]` | Apply the suggested fixes of build errors. `--code M0004,M0005` limits it to those codes. |
| `marmot explain CODE` | Describe an error code. With no code, list all of them. |
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
| `MARMOT_OUTPUT` | Diagnostic output: `json`, `human` or `plain`. See below. |
| `NO_COLOR` | Turn off color in human output. |
| `MARMOT_TIER0=0` | Dev builds use LLVM instead of the fast code generator (the default). Slower builds, faster code. |

## Error messages

Every error has a stable code (`M0002`) that `marmot explain` describes. There are three output
modes:

| Mode | When | What it looks like |
| --- | --- | --- |
| human | stderr is a terminal | The `file:line:col: message` line, then the source with carets under the exact span, labels, notes, and suggestions with a diff of the fix. At most 10 errors, sorted by file and line, then a summary. |
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
| `code` | Stable code, `M0000` when there is no more specific one. |
| `message` | The message, as in plain mode. |
| `span` | Primary location. Lines and columns are 1-based, columns count bytes, `endCol` is exclusive. Absent location: `file` is empty. |
| `labels` | Secondary spans, each `{span, text}`. |
| `notes`, `help` | Lists of strings. |
| `fixes` | Machine applicable fixes, each `{title, edits}`; an edit is `{file, span, newText}` and replaces the span (an empty span inserts). Applying every edit of one fix resolves the diagnostic. |
| `related` | Other locations, each `{span, message}`, for example the declaration of a called function. |
| `context` | What you would open the file for: `function` (enclosing signature), `lines` (source around the span, `{line, text}`), `types` (one line definitions of the types the message names). |
| `bundle` | Internal compiler errors only: a directory with the inputs for a bug report. |
| `explain` | The command that describes the code. |

`result` fields: `command`, `ok`, `errors`, `warnings`, `output` (the file written, on success),
`durationMs`, `exitCode`.

In json mode all errors are listed (no cap), duplicates removed, in a stable order. `marmot fix
--json` writes one `{"kind":"fix","file","code","title","line"}` line per applied fix, then the
remaining diagnostics and the result. `marmot explain --json CODE` writes `{code, title, text,
example, fix}` to stdout.

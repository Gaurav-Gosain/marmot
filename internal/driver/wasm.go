package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// wasmTool finds a wasm tool: $env, beside marmot's clang, Homebrew's lld
// (wasm-ld), or PATH.
func wasmTool(name, env string) string {
	if p := os.Getenv(env); p != "" {
		return p
	}
	for _, p := range []string{llvmTool(name), "/opt/homebrew/opt/lld@22/bin/" + name, "/usr/local/opt/lld@22/bin/" + name} {
		if fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// asyncifyFlags is wasm-opt's Asyncify pass as the runtime expects it
// (runtime/core/Makefile ASYNCIFY_FLAGS): goroutines switch by unwinding
// through asyncify.start_unwind, and ctx_run, which drives the switch, is
// left out of the rewrite. Asyncify runs before any optimization.
var asyncifyFlags = []string{"--asyncify", "--pass-arg=asyncify-imports@asyncify.start_unwind", "--pass-arg=asyncify-removelist@ctx_run"}

// linkWasm links a wasip1/wasm program: wasm-ld (the runtime has _start),
// then wasm-opt's Asyncify, then wasm-opt's optimizer at -O 2 (-O2) and in
// -release (-Oz). Names stay in the module except with -s.
func (b *build) linkWasm(objs []string, rt, out string) error {
	ld := wasmTool("wasm-ld", "MARMOT_WASMLD")
	opt := wasmTool("wasm-opt", "MARMOT_WASMOPT")
	switch {
	case ld == "":
		return fmt.Errorf("-target wasip1/wasm needs wasm-ld (LLVM's lld: brew install lld@22, or set MARMOT_WASMLD)")
	case opt == "":
		return fmt.Errorf("-target wasip1/wasm needs wasm-opt (binaryen: brew install binaryen, or set MARMOT_WASMOPT)")
	}
	raw := filepath.Join(b.work, "link.wasm")
	args := append([]string{"-o", raw}, objs...)
	args = append(args, rt)
	if err := b.run(ld, args...); err != nil {
		return err
	}
	oargs := append([]string{raw, "-o", out}, asyncifyFlags...)
	switch {
	case b.cfg.Release:
		oargs = append(oargs, "-Oz")
	case b.cfg.OptLevel != "" && b.cfg.OptLevel != "0":
		oargs = append(oargs, "-O2")
	}
	if !b.cfg.Strip {
		oargs = append(oargs, "-g") // keep the name section
	}
	return b.run(opt, oargs...)
}

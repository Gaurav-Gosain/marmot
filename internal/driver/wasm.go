package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/wasmfile"
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
	hostCallers := os.Getenv("MARMOT_WASM_HOSTCALLERS") == "1"
	if hostCallers {
		// The host's stack and pc table imports (marmot.callers,
		// marmot.pcln) for tracebacks: only hosts that provide them (the
		// playground) ask for this.
		cl := b.rtLib("wasm-callers.o")
		if !fileExists(cl) {
			return fmt.Errorf("MARMOT_WASM_HOSTCALLERS=1 needs %s (the runtime build for wasip1/wasm)", cl)
		}
		args = append(args, cl)
	}
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
	oargs = append(oargs, "-g") // names stay until clean, which -s strips
	if err := b.run(opt, oargs...); err != nil {
		return err
	}
	// The pc table from the final code (wasm-opt -g keeps the DWARF up
	// to date), before -s strips the DWARF.
	var tbl []byte
	if hostCallers && !b.cfg.Strip && fileExists(b.pclnTool()) {
		p := filepath.Join(b.work, "pcln.bin")
		if err := b.run(b.pclnTool(), append(b.pclnArgs(p), out)...); err != nil {
			b.logf("pcln: %v (no table)", err)
		} else if tbl, err = os.ReadFile(p); err != nil {
			return err
		}
	}
	if err := cleanWasm(out, b.cfg.Strip); err != nil {
		return err
	}
	if tbl == nil {
		return nil
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return err
	}
	if data, err = wasmfile.AppendPcln(data, tbl); err != nil {
		return fmt.Errorf("%s: %v", out, err)
	}
	return os.WriteFile(out, data, 0o755)
}

// cleanWasm rewrites the linked module: only _start and the memory stay
// exported (Asyncify exports its own control functions, which the runtime
// calls inside the module), a producers section names marmot, and -s drops
// the names section.
func cleanWasm(path string, strip bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data, err = wasmfile.Clean(data, wasmfile.Options{
		Keep:       []string{"_start", "memory"},
		StripNames: strip,
		Producers:  [][2]string{{"marmot", "devel"}},
	})
	if err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	return os.WriteFile(path, data, 0o755)
}

// buildWat implements -emit wat: the program linked as for -emit exe, then
// printed as WebAssembly text (binaryen's wasm-dis), for /play and reading.
func buildWat(cfg Config) (string, error) {
	if _, arch, _ := strings.Cut(cfg.Target, "/"); arch != "wasm" {
		return "", fmt.Errorf("-emit wat needs -target wasip1/wasm")
	}
	dis := wasmTool("wasm-dis", "MARMOT_WASMDIS")
	if dis == "" {
		return "", fmt.Errorf("-emit wat needs wasm-dis (binaryen: brew install binaryen, or set MARMOT_WASMDIS)")
	}
	tmp, err := os.MkdirTemp("", "marmot-wat-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	out := cfg.Output
	cfg.Emit, cfg.Output = "exe", filepath.Join(tmp, "prog.wasm")
	wasm, err := Build(cfg)
	if err != nil {
		return "", err
	}
	if out == "" {
		out = strings.TrimSuffix(filepath.Base(cfg.Dir), "/") + ".wat"
		if cfg.Dir == "" || cfg.Dir == "." {
			wd, _ := os.Getwd()
			out = filepath.Base(wd) + ".wat"
		}
	}
	if b, err := exec.Command(dis, wasm, "-o", out).CombinedOutput(); err != nil {
		return "", fmt.Errorf("wasm-dis: %v: %s", err, b)
	}
	return out, nil
}

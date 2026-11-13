package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/bind"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
)

type bindFlags struct {
	config, out, pkg, report *string
	pkgConfig                *string
}

func newBindFlags() (*flag.FlagSet, *bindFlags) {
	fs := flag.NewFlagSet("bind", flag.ContinueOnError)
	f := &bindFlags{}
	f.config = fs.String("config", "", "bind.toml with prefixes, renames, status codes and ownership (default: bind.toml beside the output, if any)")
	f.out = fs.String("o", "", "write the Go file here (default: standard output)")
	f.pkg = fs.String("pkg", "", "Go package name (default: from the header name)")
	f.report = fs.String("report", "", "write the report (what was not bound and why) here too")
	f.pkgConfig = fs.String("pkg-config", "", "pkg-config name of the library: its cflags, and #cgo pkg-config in the output")
	return fs, f
}

// cmdBind implements marmot bind [flags] HEADER: Go bindings from a C
// header, as a plain cgo file.
func cmdBind(args []string) int {
	fs, bf := newBindFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("bind", err)
	}
	cfgFile := *bf.config
	if cfgFile == "" && *bf.out != "" {
		if p := filepath.Join(filepath.Dir(*bf.out), "bind.toml"); func() bool { _, err := os.Stat(p); return err == nil }() {
			cfgFile = p
		}
	}
	cfg, err := bind.LoadConfig(cfgFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot bind:", err)
		return 2
	}
	if fs.NArg() == 1 {
		cfg.Header = fs.Arg(0)
	}
	if cfg.Header == "" {
		return argError((outputFlags{}).reporter("bind"), "bind", "marmot bind [-config bind.toml] [-o file.go] [-pkg name] [-pkg-config name] HEADER", fs.Args(), "HEADER")
	}
	if *bf.pkg != "" {
		cfg.Package = *bf.pkg
	}
	if *bf.pkgConfig != "" {
		cfg.PkgConfig = *bf.pkgConfig
	}
	cflags := append([]string{}, cfg.CFlags...)
	if cfg.PkgConfig != "" {
		out, err := exec.Command("pkg-config", "--cflags", cfg.PkgConfig).Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "marmot bind: pkg-config --cflags %s: %v\n", cfg.PkgConfig, err)
			return 1
		}
		cflags = append(cflags, strings.Fields(string(out))...)
	}
	h, err := bind.Parse(driver.LLVMTool("clang"), cfg.Header, cflags, cfg.Files)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot bind:", err)
		return 1
	}
	include := cfg.Header
	if filepath.IsAbs(include) {
		include = filepath.Base(include)
	}
	src, report, err := bind.Generate(cfg, h, include)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot bind:", err)
		return 1
	}
	if *bf.out == "" {
		os.Stdout.Write(src)
	} else if err := os.WriteFile(*bf.out, src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "marmot bind:", err)
		return 1
	}
	text := ""
	for _, r := range report {
		text += "- " + r + "\n"
	}
	if text != "" {
		fmt.Fprintf(os.Stderr, "marmot bind: %d notes (settle them in bind.toml):\n%s", len(report), text)
	}
	if *bf.report != "" {
		if err := os.WriteFile(*bf.report, []byte(text), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "marmot bind:", err)
			return 1
		}
	}
	return 0
}

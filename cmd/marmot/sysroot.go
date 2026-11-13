package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/driver"
	"github.com/Gaurav-Gosain/marmot/internal/sysroot"
)

// cmdSysroot implements marmot sysroot add|list|remove [-libc musl]
// [GOOS/GOARCH]: the C headers and libraries of a target for cross
// compiling cgo packages and linking (-libc musl: for -static).
func cmdSysroot(args []string) int {
	root, err := driver.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 2
	}
	usage := func() int {
		fmt.Fprintf(os.Stderr, "usage: marmot sysroot add|remove [-libc musl] GOOS/GOARCH, or marmot sysroot list (targets: %s)\n", strings.Join(sysroot.Supported(), ", "))
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	musl := false
	if len(args) >= 3 && args[1] == "-libc" {
		switch args[2] {
		case "musl":
			musl = true
		case "glibc":
		default:
			fmt.Fprintf(os.Stderr, "marmot sysroot: -libc %s: glibc (the default) or musl\n", args[2])
			return 2
		}
		args = append(args[:1], args[3:]...)
	}
	target := func() (string, string, bool) {
		if len(args) != 2 {
			return "", "", false
		}
		goos, goarch, ok := strings.Cut(args[1], "/")
		return goos, goarch, ok
	}
	switch args[0] {
	case "add":
		goos, goarch, ok := target()
		if !ok {
			return usage()
		}
		log := func(s string) { fmt.Fprintln(os.Stderr, s) }
		var m *sysroot.Manifest
		if musl {
			m, err = sysroot.AddMusl(root, goos, goarch, driver.LLVMTool("clang"), log)
			goarch = sysroot.MuslArch(goarch)
		} else {
			m, err = sysroot.Add(root, goos, goarch, log)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "marmot sysroot:", err)
			return 1
		}
		fmt.Printf("%s: %s in %s\n", m.Target, describe(m), sysroot.Dir(root, goos, goarch))
	case "remove":
		goos, goarch, ok := target()
		if !ok {
			return usage()
		}
		if musl {
			goarch = sysroot.MuslArch(goarch)
		}
		if err := sysroot.Remove(root, goos, goarch); err != nil {
			fmt.Fprintln(os.Stderr, "marmot sysroot:", err)
			return 1
		}
	case "list":
		for _, t := range sysroot.Supported() {
			goos, goarch, _ := strings.Cut(t, "/")
			if m, err := sysroot.ReadManifest(root, goos, goarch); err == nil {
				fmt.Printf("%-13s %s\n", t, describe(m))
			} else {
				fmt.Printf("%-13s not installed (marmot sysroot add %s)\n", t, t)
			}
			if m, err := sysroot.ReadManifest(root, goos, sysroot.MuslArch(goarch)); err == nil {
				fmt.Printf("%-13s %s\n", t+" musl", describe(m))
			}
		}
	default:
		return usage()
	}
	return 0
}

func describe(m *sysroot.Manifest) string {
	var ps []string
	for _, p := range m.Packages {
		ps = append(ps, p.Name+" "+p.Version)
	}
	return strings.Join(ps, ", ") + " (" + m.Source + ")"
}

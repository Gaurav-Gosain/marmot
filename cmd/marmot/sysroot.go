package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/driver"
	"github.com/Gaurav-Gosain/marmot/internal/sysroot"
)

// cmdSysroot implements marmot sysroot add|list|remove [GOOS/GOARCH]: the
// C headers of a target for cross compiling cgo packages.
func cmdSysroot(args []string) int {
	root, err := driver.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 2
	}
	usage := func() int {
		fmt.Fprintf(os.Stderr, "usage: marmot sysroot add|remove GOOS/GOARCH, or marmot sysroot list (targets: %s)\n", strings.Join(sysroot.Supported(), ", "))
		return 2
	}
	if len(args) == 0 {
		return usage()
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
		m, err := sysroot.Add(root, goos, goarch, func(s string) { fmt.Fprintln(os.Stderr, s) })
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

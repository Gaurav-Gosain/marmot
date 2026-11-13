// Command marmot-check runs the analyzers behind marmot check: go vet,
// the golangci-lint standard set and staticcheck, the go fix modernizers,
// formatting, go mod tidy, cross builds, dead code and vulnerabilities.
// marmot runs it for `marmot check`; it is a separate binary so that the
// analyzers do not slow down the start of every other marmot command.
package main

import (
	"fmt"
	"os"

	"github.com/Gaurav-Gosain/marmot/internal/check"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: marmot-check check [flags] [packages]")
		os.Exit(2)
	}
	version := os.Getenv("MARMOT_VERSION")
	switch os.Args[1] {
	case "check":
		os.Exit(check.Main(os.Args[2:], version))
	case "fix":
		os.Exit(check.FixMain(os.Args[2:], version))
	default:
		fmt.Fprintf(os.Stderr, "marmot-check: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

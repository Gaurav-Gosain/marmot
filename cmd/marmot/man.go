package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// manCommand writes a man page for marmot and its commands in roff.
func manCommand(root func() *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:    "man",
		Short:  "Print the marmot man page (roff)",
		Long:   "Print the man page in roff. Install it with: marmot man > ~/.local/share/man/man1/marmot.1",
		Args:   cobra.NoArgs,
		Hidden: true,
		Run: func(*cobra.Command, []string) {
			writeMan(os.Stdout, root())
		},
	}
}

func roffEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\e`)
	var b strings.Builder
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, ".") || strings.HasPrefix(l, "'") {
			l = `\&` + l
		}
		b.WriteString(l)
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func writeMan(w io.Writer, root *cobra.Command) {
	fmt.Fprintf(w, ".TH MARMOT 1 \"\" \"%s\" \"User Commands\"\n", version)
	fmt.Fprintf(w, ".SH NAME\nmarmot \\- %s\n", roffEscape(root.Short))
	fmt.Fprintf(w, ".SH SYNOPSIS\n.B marmot\n.I command\n[flags] [packages]\n")
	fmt.Fprintf(w, ".SH DESCRIPTION\n%s\n", roffEscape(root.Long))
	fmt.Fprintf(w, ".SH COMMANDS\n")
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		fmt.Fprintf(w, ".TP\n.B marmot %s\n%s\n", roffEscape(c.Use), roffEscape(c.Short))
		if c.Long != "" {
			fmt.Fprintf(w, ".IP\n%s\n", roffEscape(c.Long))
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "help" {
				return
			}
			fmt.Fprintf(w, ".IP\n.B \\-%s\n%s\n", f.Name, roffEscape(f.Usage))
		})
	}
	fmt.Fprintf(w, ".SH EXAMPLES\n.nf\n%s\n.fi\n", roffEscape(root.Example))
	fmt.Fprintf(w, ".SH ENVIRONMENT\n")
	for _, e := range [][2]string{
		{"MARMOT_OUTPUT", "json, human or plain: the form of diagnostics and runtime errors"},
		{"MARMOT_CACHE_MAX_MB", "size cap of the build cache"},
		{"MARMOT_RELEASE", "1 makes -release the default"},
		{"MARMOT_TIER0", "0 builds dev mode with LLVM instead of the fast code generator"},
		{"NO_COLOR", "no color in human output"},
	} {
		fmt.Fprintf(w, ".TP\n.B %s\n%s\n", e[0], roffEscape(e[1]))
	}
}

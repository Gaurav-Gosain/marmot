package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	var verbose bool
	var count int
	root := &cobra.Command{Use: "app", Short: "demo app", Long: "A demo application."}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	echo := &cobra.Command{
		Use: "echo [words]", Short: "echo words", Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			s := strings.Join(args, " ")
			if verbose {
				s = "[v] " + s
			}
			for i := 0; i < count; i++ {
				fmt.Fprintln(cmd.OutOrStdout(), s)
			}
		},
	}
	echo.Flags().IntVarP(&count, "times", "t", 1, "repeat")
	root.AddCommand(echo)
	root.AddCommand(&cobra.Command{Use: "fail", RunE: func(*cobra.Command, []string) error { return fmt.Errorf("boom") }})
	root.AddCommand(&cobra.Command{Use: "hidden", Hidden: true, Run: func(*cobra.Command, []string) {}})
	for _, args := range [][]string{{"echo", "hello", "world"}, {"-v", "echo", "-t", "2", "x"}, {"echo"}, {"fail"}, {"nope"}, {"echo", "--bad"}, {"--help"}, {"echo", "--help"}, {"help", "echo"}, {}} {
		out.Reset()
		root.SetArgs(args)
		err := root.Execute()
		fmt.Printf("== %v err=%v\n%s", args, err, out.String())
		count, verbose = 1, false
	}
	fmt.Println(root.UsageString() != "", len(root.Commands()), echo.CommandPath(), func() bool { c, _, e := root.Find([]string{"echo"}); return c != nil && e == nil }())
}

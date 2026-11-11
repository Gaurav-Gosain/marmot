package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
)

func main() {
	fs := pflag.NewFlagSet("demo", pflag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	name := fs.StringP("name", "n", "anon", "user name")
	count := fs.IntP("count", "c", 1, "repeat count")
	verbose := fs.BoolP("verbose", "v", false, "verbose output")
	ratio := fs.Float64("ratio", 0.5, "a ratio")
	dur := fs.Duration("wait", time.Second, "wait time")
	tags := fs.StringSlice("tag", nil, "tags")
	ints := fs.IntSlice("num", nil, "numbers")
	m := fs.StringToString("kv", nil, "pairs")
	level := fs.CountP("level", "l", "level")
	fs.MarkHidden("ratio")
	err := fs.Parse([]string{"-n", "bob", "--count=3", "-vv", "--tag", "a,b", "--tag=c", "--num", "1,2", "--kv", "x=1,y=2", "-lll", "--wait", "90s", "pos1", "--", "-notflag"})
	fmt.Println(err, *name, *count, *verbose, *ratio, *dur, *tags, *ints, *m, *level)
	fmt.Println(fs.Args(), fs.NArg(), fs.NFlag(), fs.Changed("name"), fs.Changed("ratio"))
	fs.Usage()
	fmt.Print(fs.FlagUsages())
	for _, bad := range [][]string{{"--nope"}, {"--count", "x"}, {"-c"}, {"--count=1.5"}, {"--wait", "xyz"}, {"-z"}} {
		f2 := pflag.NewFlagSet("bad", pflag.ContinueOnError)
		f2.SetOutput(os.Stdout)
		f2.Usage = func() {}
		f2.Int("count", 0, "")
		f2.IntP("c2", "c", 0, "")
		f2.Duration("wait", 0, "")
		fmt.Println(bad, f2.Parse(bad))
	}
	fs.VisitAll(func(f *pflag.Flag) { fmt.Print(f.Name, ":", f.Value.Type(), " ") })
	fmt.Println()
}

package main

import (
	"flag"
	"fmt"
	"time"
)

func main() {
	fs := flag.NewFlagSet("prog", flag.ContinueOnError)
	n := fs.Int("n", 5, "count")
	name := fs.String("name", "def", "name")
	v := fs.Bool("v", false, "verbose")
	d := fs.Duration("d", time.Second, "dur")
	f := fs.Float64("f", 1.5, "float")
	var list []string
	fs.Func("item", "item", func(s string) error { list = append(list, s); return nil })
	err := fs.Parse([]string{"-n", "9", "--name=x y", "-v", "-d", "2m", "-item", "a", "-item=b", "rest1", "-f", "2"})
	fmt.Println(err, *n, *name, *v, *d, *f, list, fs.Args(), fs.NArg(), fs.NFlag())
	fs2 := flag.NewFlagSet("p2", flag.ContinueOnError)
	fs2.SetOutput(nopWriter{})
	fs2.Int("x", 0, "x")
	fmt.Println(fs2.Parse([]string{"-x", "abc"}))
	fmt.Println(fs2.Parse([]string{"-nope"}))
	fmt.Println(fs2.Parse([]string{"--", "-x", "1"}), fs2.Args())
	fs.VisitAll(func(f *flag.Flag) { fmt.Print(f.Name, "=", f.Value, " ") })
	fmt.Println()
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

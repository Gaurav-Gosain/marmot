// //go:embed in the main package (string, []byte, embed.FS) and in a
// package of the module cache: x/tools analyzers embed their doc.go.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"golang.org/x/tools/go/analysis/passes/appends"
	"golang.org/x/tools/go/analysis/passes/assign"
)

//go:embed embed_data/a.txt
var a string

//go:embed embed_data/b.txt
var b []byte

//go:embed embed_data
var dir embed.FS

func main() {
	fmt.Printf("%q %q\n", a, b)
	fs.WalkDir(dir, ".", func(p string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			data, _ := dir.ReadFile(p)
			fmt.Printf("%s: %q\n", p, data)
		}
		return nil
	})
	for _, an := range []string{appends.Analyzer.Doc, assign.Analyzer.Doc} {
		fmt.Println(strings.SplitN(an, "\n", 2)[0])
	}
}

package main

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

const src = "# Title *with* emphasis\n\nA paragraph with `code`, **bold**, a [link](http://example.com \"t\") and an image ![alt](x.png).\n\n- item one\n- item two\n  - nested\n\n1. first\n2. second\n\n> quote line\n> more\n\n```go\nfmt.Println(\"hi\")\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nHard  \nbreak and <b>raw html</b> & entities &copy;\n\n---\n\n~~strike~~ https://auto.link\n"

func main() {
	var buf bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID()), goldmark.WithRendererOptions(html.WithUnsafe()))
	err := md.Convert([]byte(src), &buf)
	fmt.Println(err)
	fmt.Print(buf.String())
	var plain bytes.Buffer
	goldmark.Convert([]byte("# H\n<script>x</script>\n\n[js](javascript:alert(1))\n"), &plain)
	fmt.Print(plain.String())
	n := goldmark.DefaultParser().Parse(text.NewReader([]byte("a *b* c\n\n> q")))
	cnt := 0
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		cnt++
	}
	fmt.Println(cnt, n.ChildCount())
}

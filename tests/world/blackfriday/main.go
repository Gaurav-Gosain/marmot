package main

import (
	"fmt"

	"github.com/russross/blackfriday/v2"
)

const src = "# Title\n\nSome *emphasis*, **strong**, `code`, and a [link](http://x.org).\n\n* one\n* two\n\n1. a\n2. b\n\n> quote\n\n```\nfmt.Println(1)\n```\n\n| h1 | h2 |\n|----|----|\n| a  | b  |\n\nline  \nbreak\n\n---\n\nhttp://auto.link <b>raw</b>\n"

func main() {
	out := blackfriday.Run([]byte(src))
	fmt.Print(string(out))
	out = blackfriday.Run([]byte(src), blackfriday.WithExtensions(blackfriday.NoExtensions))
	fmt.Println(len(out))
	r := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{Flags: blackfriday.Smartypants | blackfriday.HrefTargetBlank})
	out = blackfriday.Run([]byte("\"quoted\" -- text... [l](http://a)\n"), blackfriday.WithRenderer(r))
	fmt.Print(string(out))
	n := blackfriday.New().Parse([]byte("a\n\n- b\n- c\n"))
	cnt := 0
	n.Walk(func(node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		if entering {
			cnt++
		}
		return blackfriday.GoToNext
	})
	fmt.Println(cnt)
}

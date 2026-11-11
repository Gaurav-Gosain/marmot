package specialize

import "testing"

// regexpProgram runs every Find, Match, Replace and Split method on many
// constant patterns and inputs (Unicode, invalid UTF-8, empty matches,
// anchors, word boundaries, folding, lazy and greedy repeats, alternation
// priority, nested captures, inputs long enough to exhaust the step budget).
const regexpProgram = `package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var out strings.Builder

func run(name string, re *regexp.Regexp, inputs []string) {
	for _, in := range inputs {
		b := []byte(in)
		fmt.Fprintf(&out, "%s %q: %v %q %v %q %v\n", name, in, re.MatchString(in), re.FindString(in), re.FindStringIndex(in), re.FindStringSubmatch(in), re.FindStringSubmatchIndex(in))
		fmt.Fprintf(&out, "  all %q %v %v\n", re.FindAllString(in, -1), re.FindAllStringIndex(in, 3), re.FindAllStringSubmatchIndex(in, -1))
		fmt.Fprintf(&out, "  repl %q %q %q\n", re.ReplaceAllString(in, "<$0|$1>"), re.ReplaceAllLiteralString(in, "#"), re.Split(in, -1))
		fmt.Fprintf(&out, "  bytes %v %q %v %q\n", re.Match(b), re.Find(b), re.FindAllIndex(b, -1), re.ReplaceAll(b, []byte("[$1]")))
	}
}

func main() {
	inputs := []string{
		"", "a", "abc", "aaa", "abcabc", "xyz", "Hello, World", "hello world foo_bar baz9",
		"2026-11-12 12:34:56 INFO user42@example.com GET /api/v2/items/17 200 33ms",
		"user1@ user22@x user@ userX@ user333@",
		"日本語 テキスト ab日c", "café CAFÉ K k K", "bad\xff\xfeutf8 a\xc3", "line1\nline2\r\nline3",
		"   leading and trailing   ", "a.b*c+d?e(f)[g]{h}", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab",
		strings.Repeat("ab", 200) + "c", strings.Repeat("x", 3000) + "user7@" + strings.Repeat("y", 100),
	}
	run("lit", regexp.MustCompile("abc"), inputs)
	run("alt", regexp.MustCompile("a|ab|abc"), inputs)
	run("star", regexp.MustCompile("a*"), inputs)
	run("lazy", regexp.MustCompile("a+?b*?"), inputs)
	run("caps", regexp.MustCompile("(a)(b(c))?"), inputs)
	run("log", regexp.MustCompile(` + "`" + `(\d+)-(\d+)-(\d+) .* (\w+)@example\.com GET (/\S+) (\d{3}) (\d+)ms` + "`" + `), inputs)
	run("mail", regexp.MustCompile(` + "`" + `user(\d+)@` + "`" + `), inputs)
	run("anchors", regexp.MustCompile(` + "`" + `^\w+|\w+$` + "`" + `), inputs)
	run("multi", regexp.MustCompile(` + "`" + `(?m)^line\d$` + "`" + `), inputs)
	run("word", regexp.MustCompile(` + "`" + `\b\w` + "`" + `), inputs)
	run("fold", regexp.MustCompile(` + "`" + `(?i)café|k` + "`" + `), inputs)
	run("class", regexp.MustCompile(` + "`" + `[^a-z\s]+` + "`" + `), inputs)
	run("unicode", regexp.MustCompile(` + "`" + `\p{Han}+|[\x{30A0}-\x{30FF}]+` + "`" + `), inputs)
	run("dot", regexp.MustCompile(` + "`" + `a.c|.\z` + "`" + `), inputs)
	run("dotnl", regexp.MustCompile(` + "`" + `(?s)e.l` + "`" + `), inputs)
	run("empty", regexp.MustCompile(` + "`" + `` + "`" + `), inputs[:6])
	run("opt", regexp.MustCompile(` + "`" + `x*|b` + "`" + `), inputs)
	run("nested", regexp.MustCompile(` + "`" + `((a|b)+)(c)?` + "`" + `), inputs)
	run("patho", regexp.MustCompile(` + "`" + `(a*)*b` + "`" + `), inputs)
	run("patho2", regexp.MustCompile(` + "`" + `(a|aa)+$` + "`" + `), inputs)
	run("repeat", regexp.MustCompile(` + "`" + `(ab){2,3}` + "`" + `), inputs)
	run("bad", regexp.MustCompile("�+"), inputs)
	re, err := regexp.Compile(` + "`" + `(\w+)\s(\w+)` + "`" + `)
	fmt.Fprintln(&out, err)
	run("compile", re, inputs)
	_, err = regexp.Compile("a(b")
	fmt.Fprintln(&out, err)
	l := regexp.MustCompile("a+|a+b")
	l.Longest()
	run("longest", l, inputs)
	c := regexp.MustCompile("(x)(y)?").Copy()
	run("copy", c, inputs)
	os.Stdout.WriteString(out.String())
}
`

func TestRegexpDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, regexpProgram, 20)
}

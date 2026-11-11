// A lexer for a Go-like language: keywords, identifiers, numbers, strings, runes, comments,
// operators, automatic semicolon insertion, and position tracking.
package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Kind int

const (
	ILLEGAL Kind = iota
	EOF
	IDENT
	INT
	FLOAT
	CHAR
	STRING
	COMMENT
	OP
	KEYWORD
	SEMI
)

var kindNames = [...]string{"ILLEGAL", "EOF", "IDENT", "INT", "FLOAT", "CHAR", "STRING", "COMMENT", "OP", "KEYWORD", "SEMI"}

func (k Kind) String() string { return kindNames[k] }

type Pos struct{ Line, Col int }

type Token struct {
	Kind Kind
	Text string
	Pos  Pos
}

var keywords = map[string]bool{}

func init() {
	for _, k := range strings.Fields("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var") {
		keywords[k] = true
	}
}

var operators = []string{
	"<<=", ">>=", "&^=", "...", "&&", "||", "<-", "++", "--", "==", "!=", "<=", ">=", ":=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<", ">>", "&^",
	"+", "-", "*", "/", "%", "&", "|", "^", "<", ">", "=", "!", "(", ")", "[", "]", "{", "}", ",", ";", ".", ":", "~",
}

type Lexer struct {
	src       string
	off       int
	line, col int
	lastSemi  bool
	errors    []string
}

func NewLexer(src string) *Lexer { return &Lexer{src: src, line: 1, col: 1} }

func (l *Lexer) peekRune() (rune, int) {
	if l.off >= len(l.src) {
		return 0, 0
	}
	return utf8.DecodeRuneInString(l.src[l.off:])
}

func (l *Lexer) advance(n int) {
	for i := 0; i < n; {
		r, size := utf8.DecodeRuneInString(l.src[l.off:])
		l.off += size
		i += size
		if r == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
	}
}

func (l *Lexer) errorf(p Pos, f string, a ...any) {
	l.errors = append(l.errors, fmt.Sprintf("%d:%d: %s", p.Line, p.Col, fmt.Sprintf(f, a...)))
}

func isLetter(r rune) bool { return r == '_' || unicode.IsLetter(r) }

func (l *Lexer) Next() Token {
	for {
		r, size := l.peekRune()
		if size == 0 {
			if l.lastSemi {
				l.lastSemi = false
				return Token{SEMI, "\n", Pos{l.line, l.col}}
			}
			return Token{EOF, "", Pos{l.line, l.col}}
		}
		if r == '\n' && l.lastSemi {
			p := Pos{l.line, l.col}
			l.advance(1)
			l.lastSemi = false
			return Token{SEMI, "\n", p}
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			l.advance(size)
			continue
		}
		break
	}
	pos := Pos{l.line, l.col}
	rest := l.src[l.off:]
	r, _ := l.peekRune()
	switch {
	case strings.HasPrefix(rest, "//"):
		end := strings.IndexByte(rest, '\n')
		if end < 0 {
			end = len(rest)
		}
		t := Token{COMMENT, rest[:end], pos}
		l.advance(end)
		return t
	case strings.HasPrefix(rest, "/*"):
		end := strings.Index(rest[2:], "*/")
		if end < 0 {
			l.errorf(pos, "unterminated comment")
			t := Token{ILLEGAL, rest, pos}
			l.advance(len(rest))
			return t
		}
		text := rest[:end+4]
		wasSemi := l.lastSemi
		l.advance(len(text))
		if strings.Contains(text, "\n") && wasSemi {
			l.lastSemi = false
			return Token{SEMI, "\n", pos}
		}
		return Token{COMMENT, text, pos}
	case isLetter(r):
		n := 0
		for _, c := range rest {
			if !isLetter(c) && !unicode.IsDigit(c) {
				break
			}
			n += utf8.RuneLen(c)
		}
		text := rest[:n]
		l.advance(n)
		if keywords[text] {
			l.lastSemi = text == "break" || text == "continue" || text == "fallthrough" || text == "return"
			return Token{KEYWORD, text, pos}
		}
		l.lastSemi = true
		return Token{IDENT, text, pos}
	case unicode.IsDigit(r) || r == '.' && len(rest) > 1 && rest[1] >= '0' && rest[1] <= '9':
		return l.number(pos, rest)
	case r == '"':
		return l.quoted(pos, rest, '"', STRING)
	case r == '\'':
		return l.quoted(pos, rest, '\'', CHAR)
	case r == '`':
		end := strings.IndexByte(rest[1:], '`')
		if end < 0 {
			l.errorf(pos, "unterminated raw string")
			l.advance(len(rest))
			return Token{ILLEGAL, rest, pos}
		}
		t := Token{STRING, rest[:end+2], pos}
		l.advance(end + 2)
		l.lastSemi = true
		return t
	}
	for _, op := range operators {
		if strings.HasPrefix(rest, op) {
			l.advance(len(op))
			l.lastSemi = op == ")" || op == "]" || op == "}" || op == "++" || op == "--"
			if op == ";" {
				l.lastSemi = false
				return Token{SEMI, ";", pos}
			}
			return Token{OP, op, pos}
		}
	}
	l.errorf(pos, "illegal character %q", r)
	l.advance(utf8.RuneLen(r))
	return Token{ILLEGAL, string(r), pos}
}

func (l *Lexer) number(pos Pos, rest string) Token {
	i := 0
	isFloat := false
	if strings.HasPrefix(rest, "0x") || strings.HasPrefix(rest, "0X") {
		i = 2
		for i < len(rest) && strings.IndexByte("0123456789abcdefABCDEF_", rest[i]) >= 0 {
			i++
		}
	} else {
		for i < len(rest) && (rest[i] >= '0' && rest[i] <= '9' || rest[i] == '_') {
			i++
		}
		if i < len(rest) && rest[i] == '.' {
			isFloat = true
			i++
			for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
				i++
			}
		}
		if i < len(rest) && (rest[i] == 'e' || rest[i] == 'E') {
			j := i + 1
			if j < len(rest) && (rest[j] == '+' || rest[j] == '-') {
				j++
			}
			if j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				isFloat = true
				for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
					j++
				}
				i = j
			}
		}
	}
	k := INT
	if isFloat {
		k = FLOAT
	}
	t := Token{k, rest[:i], pos}
	l.advance(i)
	l.lastSemi = true
	return t
}

func (l *Lexer) quoted(pos Pos, rest string, q byte, k Kind) Token {
	i := 1
	for i < len(rest) {
		if rest[i] == '\\' {
			i += 2
			continue
		}
		if rest[i] == q {
			t := Token{k, rest[:i+1], pos}
			l.advance(i + 1)
			l.lastSemi = true
			return t
		}
		if rest[i] == '\n' {
			break
		}
		i++
	}
	end := min(i, len(rest))
	l.errorf(pos, "unterminated %s literal", strings.ToLower(k.String()))
	t := Token{ILLEGAL, rest[:end], pos}
	l.advance(end)
	return t
}

func lex(src string) ([]Token, []string) {
	l := NewLexer(src)
	var toks []Token
	for {
		t := l.Next()
		toks = append(toks, t)
		if t.Kind == EOF {
			return toks, l.errors
		}
	}
}

const sample = `package main

import "fmt"

/* block
   comment */
const Pi = 3.14159 // pi
var (
	x, y = 0x1F, 1_000
	big  = 6.02e23
	r    = 'a'
	nl   = '\n'
	raw  = ` + "`raw\\nstring`" + `
)

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	ch := make(chan<- int, 3)
	ch <- x &^ y
	for i := range 10 {
		x <<= 1
		y++
	}
	s := "héllo \"quoted\" 世"
	fmt.Println(s, fib(10)...)
	switch {
	case x >= y && y != 0:
		break
	}
	var π, 世界 = 1, 2
}
`

func main() {
	toks, errs := lex(sample)
	counts := map[Kind]int{}
	for _, t := range toks {
		counts[t.Kind]++
		if t.Kind == COMMENT && strings.Contains(t.Text, "\n") {
			fmt.Printf("%d:%d %-8v %q\n", t.Pos.Line, t.Pos.Col, t.Kind, "multi-line comment")
			continue
		}
		fmt.Printf("%d:%d %-8v %q\n", t.Pos.Line, t.Pos.Col, t.Kind, t.Text)
	}
	fmt.Println(len(toks), "tokens, errors:", errs)
	for k := ILLEGAL; k <= SEMI; k++ {
		fmt.Printf("%v=%d ", k, counts[k])
	}
	fmt.Println()

	bad := []string{"x := \"unterminated\ny := 1", "a @ b", "/* never closed", "'c", "`raw", "1e+ 5", "x.y...z", "a<<=b>>=c&^=d"}
	for _, src := range bad {
		toks, errs := lex(src)
		var parts []string
		for _, t := range toks {
			parts = append(parts, fmt.Sprintf("%v(%s)", t.Kind, strings.TrimSpace(t.Text)))
		}
		fmt.Printf("%-28q %s %v\n", src, strings.Join(parts, " "), errs)
	}
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "v%d := f(%d, \"s%d\") + 0x%x // c%d\n", i, i, i, i*7, i)
	}
	toks, errs = lex(sb.String())
	fmt.Println(len(toks), len(errs), toks[len(toks)-2])
}

// A small expression language: tokenizer, Pratt parser, tree-walking evaluator with
// variables, user functions, closures and error reporting.
package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type TokKind int

const (
	EOF TokKind = iota
	NUM
	IDENT
	OP
	LP
	RP
	COMMA
	STR
	KW
)

type Token struct {
	Kind TokKind
	Text string
	Pos  int
}

var keywords = map[string]bool{"let": true, "fn": true, "if": true, "then": true, "else": true, "in": true}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func tokenize(src string) ([]Token, error) {
	var toks []Token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case isDigit(c) || c == '.' && i+1 < len(src) && isDigit(src[i+1]):
			j := i
			for j < len(src) && (isDigit(src[j]) || src[j] == '.') {
				j++
			}
			toks = append(toks, Token{NUM, src[i:j], i})
			i = j
		case isAlpha(c):
			j := i
			for j < len(src) && (isAlpha(src[j]) || isDigit(src[j])) {
				j++
			}
			k := IDENT
			if keywords[src[i:j]] {
				k = KW
			}
			toks = append(toks, Token{k, src[i:j], i})
			i = j
		case c == '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				j++
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated string at %d", i)
			}
			toks = append(toks, Token{STR, src[i+1 : j], i})
			i = j + 1
		case c == '(':
			toks = append(toks, Token{LP, "(", i})
			i++
		case c == ')':
			toks = append(toks, Token{RP, ")", i})
			i++
		case c == ',':
			toks = append(toks, Token{COMMA, ",", i})
			i++
		case strings.IndexByte("+-*/%^<>=!&|", c) >= 0:
			j := i + 1
			if j < len(src) && strings.Contains("== != <= >= && ||", src[i:j+1]) {
				j++
			}
			toks = append(toks, Token{OP, src[i:j], i})
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q at %d", c, i)
		}
	}
	toks = append(toks, Token{EOF, "", len(src)})
	return toks, nil
}

type Node interface{ String() string }

type Num struct{ V float64 }
type Str struct{ V string }
type Var struct{ Name string }
type Unary struct {
	Op string
	X  Node
}
type Binary struct {
	Op   string
	L, R Node
}
type Call struct {
	Fn   Node
	Args []Node
}
type If struct{ Cond, Then, Else Node }
type Let struct {
	Name      string
	Val, Body Node
}
type Lambda struct {
	Params []string
	Body   Node
}

func (n Num) String() string    { return strconv.FormatFloat(n.V, 'g', -1, 64) }
func (n Str) String() string    { return strconv.Quote(n.V) }
func (n Var) String() string    { return n.Name }
func (n Unary) String() string  { return "(" + n.Op + n.X.String() + ")" }
func (n Binary) String() string { return "(" + n.L.String() + " " + n.Op + " " + n.R.String() + ")" }
func (n Call) String() string {
	parts := make([]string, len(n.Args))
	for i, a := range n.Args {
		parts[i] = a.String()
	}
	return n.Fn.String() + "(" + strings.Join(parts, ", ") + ")"
}
func (n If) String() string {
	return "if " + n.Cond.String() + " then " + n.Then.String() + " else " + n.Else.String()
}
func (n Let) String() string {
	return "let " + n.Name + " = " + n.Val.String() + " in " + n.Body.String()
}
func (n Lambda) String() string {
	return "fn(" + strings.Join(n.Params, ", ") + ") " + n.Body.String()
}

type Parser struct {
	toks []Token
	pos  int
}

var prec = map[string]int{"||": 1, "&&": 2, "==": 3, "!=": 3, "<": 4, ">": 4, "<=": 4, ">=": 4, "+": 5, "-": 5, "*": 6, "/": 6, "%": 6, "^": 8}

func (p *Parser) cur() Token { return p.toks[p.pos] }
func (p *Parser) next() Token {
	t := p.toks[p.pos]
	if t.Kind != EOF {
		p.pos++
	}
	return t
}

func (p *Parser) expect(k TokKind, text string) {
	t := p.next()
	if t.Kind != k || (text != "" && t.Text != text) {
		panic(fmt.Errorf("expected %q at %d, got %q", text, t.Pos, t.Text))
	}
}

func (p *Parser) expr(minPrec int) Node {
	left := p.unary()
	for {
		t := p.cur()
		pr, ok := prec[t.Text]
		if t.Kind != OP || !ok || pr < minPrec {
			return left
		}
		p.next()
		nextMin := pr + 1
		if t.Text == "^" {
			nextMin = pr
		}
		right := p.expr(nextMin)
		left = Binary{t.Text, left, right}
	}
}

func (p *Parser) unary() Node {
	if t := p.cur(); t.Kind == OP && (t.Text == "-" || t.Text == "!") {
		p.next()
		return Unary{t.Text, p.expr(7)}
	}
	return p.postfix()
}

func (p *Parser) postfix() Node {
	n := p.primary()
	for p.cur().Kind == LP {
		p.next()
		var args []Node
		for p.cur().Kind != RP {
			args = append(args, p.expr(1))
			if p.cur().Kind == COMMA {
				p.next()
			} else if p.cur().Kind != RP {
				panic(fmt.Errorf("expected , or ) at %d", p.cur().Pos))
			}
		}
		p.next()
		n = Call{n, args}
	}
	return n
}

func (p *Parser) primary() Node {
	t := p.next()
	switch t.Kind {
	case NUM:
		f, err := strconv.ParseFloat(t.Text, 64)
		if err != nil {
			panic(fmt.Errorf("bad number %q at %d", t.Text, t.Pos))
		}
		return Num{f}
	case STR:
		return Str{t.Text}
	case IDENT:
		return Var{t.Text}
	case LP:
		n := p.expr(1)
		p.expect(RP, ")")
		return n
	case KW:
		switch t.Text {
		case "if":
			c := p.expr(1)
			p.expect(KW, "then")
			a := p.expr(1)
			p.expect(KW, "else")
			return If{c, a, p.expr(1)}
		case "let":
			name := p.next()
			if name.Kind != IDENT {
				panic(fmt.Errorf("expected name at %d", name.Pos))
			}
			p.expect(OP, "=")
			v := p.expr(1)
			p.expect(KW, "in")
			return Let{name.Text, v, p.expr(1)}
		case "fn":
			p.expect(LP, "(")
			var ps []string
			for p.cur().Kind != RP {
				ps = append(ps, p.next().Text)
				if p.cur().Kind == COMMA {
					p.next()
				}
			}
			p.next()
			return Lambda{ps, p.expr(1)}
		}
	}
	panic(fmt.Errorf("unexpected %q at %d", t.Text, t.Pos))
}

func parse(src string) (n Node, err error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	p := &Parser{toks: toks}
	n = p.expr(1)
	if p.cur().Kind != EOF {
		return nil, fmt.Errorf("trailing input at %d: %q", p.cur().Pos, p.cur().Text)
	}
	return n, nil
}

type Value interface{}

type Env struct {
	vars   map[string]Value
	parent *Env
}

func (e *Env) lookup(n string) (Value, bool) {
	for c := e; c != nil; c = c.parent {
		if v, ok := c.vars[n]; ok {
			return v, true
		}
	}
	return nil, false
}

type Closure struct {
	params []string
	body   Node
	env    *Env
}

type Builtin func(args []Value) (Value, error)

var steps int

func eval(n Node, env *Env) (Value, error) {
	steps++
	if steps > 200000 {
		return nil, fmt.Errorf("step limit exceeded")
	}
	switch n := n.(type) {
	case Num:
		return n.V, nil
	case Str:
		return n.V, nil
	case Var:
		if v, ok := env.lookup(n.Name); ok {
			return v, nil
		}
		return nil, fmt.Errorf("undefined: %s", n.Name)
	case Unary:
		x, err := eval(n.X, env)
		if err != nil {
			return nil, err
		}
		if n.Op == "-" {
			f, ok := x.(float64)
			if !ok {
				return nil, fmt.Errorf("cannot negate %T", x)
			}
			return -f, nil
		}
		return !truthy(x), nil
	case Binary:
		return evalBinary(n, env)
	case If:
		c, err := eval(n.Cond, env)
		if err != nil {
			return nil, err
		}
		if truthy(c) {
			return eval(n.Then, env)
		}
		return eval(n.Else, env)
	case Let:
		v, err := eval(n.Val, env)
		if err != nil {
			return nil, err
		}
		child := &Env{map[string]Value{n.Name: v}, env}
		if c, ok := v.(*Closure); ok && c.env == env {
			c.env = child
		}
		return eval(n.Body, child)
	case Lambda:
		return &Closure{n.Params, n.Body, env}, nil
	case Call:
		f, err := eval(n.Fn, env)
		if err != nil {
			return nil, err
		}
		args := make([]Value, len(n.Args))
		for i, a := range n.Args {
			if args[i], err = eval(a, env); err != nil {
				return nil, err
			}
		}
		switch f := f.(type) {
		case Builtin:
			return f(args)
		case *Closure:
			if len(args) != len(f.params) {
				return nil, fmt.Errorf("arity mismatch: want %d, got %d", len(f.params), len(args))
			}
			ce := &Env{map[string]Value{}, f.env}
			for i, p := range f.params {
				ce.vars[p] = args[i]
			}
			return eval(f.body, ce)
		}
		return nil, fmt.Errorf("not callable: %v", format(f))
	}
	return nil, fmt.Errorf("cannot evaluate %T", n)
}

func truthy(v Value) bool {
	switch v := v.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	}
	return v != nil
}

func evalBinary(n Binary, env *Env) (Value, error) {
	l, err := eval(n.L, env)
	if err != nil {
		return nil, err
	}
	if n.Op == "&&" && !truthy(l) {
		return false, nil
	}
	if n.Op == "||" && truthy(l) {
		return true, nil
	}
	r, err := eval(n.R, env)
	if err != nil {
		return nil, err
	}
	if n.Op == "&&" || n.Op == "||" {
		return truthy(r), nil
	}
	if ls, ok := l.(string); ok {
		rs, ok := r.(string)
		if !ok {
			return nil, fmt.Errorf("type mismatch: string %s %T", n.Op, r)
		}
		switch n.Op {
		case "+":
			return ls + rs, nil
		case "==":
			return ls == rs, nil
		case "!=":
			return ls != rs, nil
		case "<":
			return ls < rs, nil
		}
		return nil, fmt.Errorf("bad string operator %s", n.Op)
	}
	lf, ok1 := l.(float64)
	rf, ok2 := r.(float64)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("type mismatch: %T %s %T", l, n.Op, r)
	}
	switch n.Op {
	case "+":
		return lf + rf, nil
	case "-":
		return lf - rf, nil
	case "*":
		return lf * rf, nil
	case "/":
		if rf == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return lf / rf, nil
	case "%":
		if rf == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		return math.Mod(lf, rf), nil
	case "^":
		return math.Pow(lf, rf), nil
	case "==":
		return lf == rf, nil
	case "!=":
		return lf != rf, nil
	case "<":
		return lf < rf, nil
	case ">":
		return lf > rf, nil
	case "<=":
		return lf <= rf, nil
	case ">=":
		return lf >= rf, nil
	}
	return nil, fmt.Errorf("unknown operator %s", n.Op)
}

func format(v Value) string {
	switch v := v.(type) {
	case float64:
		return strconv.FormatFloat(v, 'g', 10, 64)
	case string:
		return strconv.Quote(v)
	case bool:
		return strconv.FormatBool(v)
	case *Closure:
		return "<fn/" + strconv.Itoa(len(v.params)) + ">"
	case Builtin:
		return "<builtin>"
	}
	return fmt.Sprint(v)
}

func globals() *Env {
	m := map[string]Value{
		"pi": math.Pi,
		"sqrt": Builtin(func(a []Value) (Value, error) {
			if len(a) != 1 {
				return nil, fmt.Errorf("sqrt wants 1 argument")
			}
			f, ok := a[0].(float64)
			if !ok || f < 0 {
				return nil, fmt.Errorf("sqrt domain error")
			}
			return math.Sqrt(f), nil
		}),
		"max": Builtin(func(a []Value) (Value, error) {
			if len(a) == 0 {
				return nil, fmt.Errorf("max of nothing")
			}
			best := a[0].(float64)
			for _, x := range a[1:] {
				best = math.Max(best, x.(float64))
			}
			return best, nil
		}),
		"len": Builtin(func(a []Value) (Value, error) {
			s, ok := a[0].(string)
			if !ok {
				return nil, fmt.Errorf("len wants a string")
			}
			return float64(len(s)), nil
		}),
	}
	return &Env{m, nil}
}

func main() {
	programs := []string{
		"1 + 2 * 3", "(1 + 2) * 3", "2 ^ 3 ^ 2", "-2 ^ 2", "10 / 4 - 1", "7 % 4 + 0.5",
		"1 < 2 && 2 < 3 || 0", "!(1 == 1)", "\"foo\" + \"bar\"", "len(\"hello\") * 2",
		"sqrt(16) + max(1, 9, 3)", "pi * 2", "let x = 5 in x * x",
		"let x = 2 in let y = x + 3 in x * y", "if 3 > 2 then \"yes\" else \"no\"",
		"let sq = fn(a) a * a in sq(7)", "let add = fn(a, b) a + b in add(3, 4)",
		"let mk = fn(n) fn(x) x + n in let add5 = mk(5) in add5(10)",
		"let fact = fn(n) if n <= 1 then 1 else n * fact(n - 1) in fact(10)",
		"let fib = fn(n) if n < 2 then n else fib(n - 1) + fib(n - 2) in fib(15)",
		"let twice = fn(f, x) f(f(x)) in twice(fn(y) y * 3, 2)",
		"let compose = fn(f, g) fn(x) f(g(x)) in compose(fn(a) a + 1, fn(b) b * 2)(5)",
		"1 / 0", "undefinedVar + 1", "1 +", "(1 + 2", "\"a\" + 1",
		"let loop = fn(n) loop(n + 1) in loop(0)", "sqrt(-4)", "3 $ 4", "fn(x) x", "sqrt",
		"let f = fn(a, b) a in f(1)", "1 2", "\"unterminated",
		"let x = 1 in let x = x + 1 in let x = x * 10 in x",
	}
	for _, src := range programs {
		steps = 0
		n, err := parse(src)
		if err != nil {
			fmt.Printf("%-60s => syntax error: %v\n", src, err)
			continue
		}
		v, err := eval(n, globals())
		if err != nil {
			fmt.Printf("%-60s => error: %v\n", src, err)
			continue
		}
		fmt.Printf("%-60s => %s\n", src, format(v))
	}
	n, _ := parse("let f = fn(x, y) x * y + 1 in f(2, 3) - -4")
	fmt.Println(n)
	names := []string{}
	for k := range globals().vars {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Println(names)
}

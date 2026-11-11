// A hand-written JSON parser, printer and path query engine.
package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Kind int

const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

func (k Kind) String() string {
	return [...]string{"null", "bool", "number", "string", "array", "object"}[k]
}

type Value struct {
	Kind Kind
	B    bool
	N    float64
	S    string
	A    []*Value
	O    map[string]*Value
	Keys []string
}

type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string { return fmt.Sprintf("parse error at %d: %s", e.Pos, e.Msg) }

type parser struct {
	s   string
	pos int
}

func Parse(s string) (v *Value, err error) {
	p := &parser{s: s}
	defer func() {
		if r := recover(); r != nil {
			pe, ok := r.(*ParseError)
			if !ok {
				panic(r)
			}
			v, err = nil, pe
		}
	}()
	p.ws()
	v = p.value()
	p.ws()
	if p.pos != len(p.s) {
		p.fail("trailing data")
	}
	return v, nil
}

func (p *parser) fail(msg string) { panic(&ParseError{p.pos, msg}) }

func (p *parser) ws() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) peek() byte {
	if p.pos >= len(p.s) {
		p.fail("unexpected end")
	}
	return p.s[p.pos]
}

func (p *parser) expect(lit string) {
	if !strings.HasPrefix(p.s[p.pos:], lit) {
		p.fail("expected " + lit)
	}
	p.pos += len(lit)
}

func (p *parser) value() *Value {
	switch c := p.peek(); {
	case c == 'n':
		p.expect("null")
		return &Value{Kind: Null}
	case c == 't':
		p.expect("true")
		return &Value{Kind: Bool, B: true}
	case c == 'f':
		p.expect("false")
		return &Value{Kind: Bool}
	case c == '"':
		return &Value{Kind: String, S: p.str()}
	case c == '[':
		return p.array()
	case c == '{':
		return p.object()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		p.fail(fmt.Sprintf("unexpected %q", c))
	}
	return nil
}

func (p *parser) number() *Value {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	digits := func() int {
		n := 0
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
			n++
		}
		return n
	}
	if digits() == 0 {
		p.fail("digit expected")
	}
	if p.pos < len(p.s) && p.s[p.pos] == '.' {
		p.pos++
		if digits() == 0 {
			p.fail("fraction digit expected")
		}
	}
	if p.pos < len(p.s) && (p.s[p.pos] == 'e' || p.s[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
			p.pos++
		}
		if digits() == 0 {
			p.fail("exponent digit expected")
		}
	}
	f, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil {
		p.pos = start
		p.fail("bad number")
	}
	return &Value{Kind: Number, N: f}
}

func (p *parser) str() string {
	p.pos++
	var sb strings.Builder
	for {
		if p.pos >= len(p.s) {
			p.fail("unterminated string")
		}
		c := p.s[p.pos]
		switch {
		case c == '"':
			p.pos++
			return sb.String()
		case c == '\\':
			p.pos++
			e := p.peek()
			p.pos++
			switch e {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case '/', '\\', '"':
				sb.WriteByte(e)
			case 'u':
				if p.pos+4 > len(p.s) {
					p.fail("short unicode escape")
				}
				n, err := strconv.ParseUint(p.s[p.pos:p.pos+4], 16, 32)
				if err != nil {
					p.fail("bad unicode escape")
				}
				p.pos += 4
				sb.WriteRune(rune(n))
			default:
				p.fail("bad escape")
			}
		case c < 0x20:
			p.fail("control character in string")
		default:
			r, size := utf8.DecodeRuneInString(p.s[p.pos:])
			if r == utf8.RuneError && size == 1 {
				p.fail("invalid utf-8")
			}
			sb.WriteString(p.s[p.pos : p.pos+size])
			p.pos += size
		}
	}
}

func (p *parser) array() *Value {
	p.pos++
	v := &Value{Kind: Array}
	p.ws()
	if p.peek() == ']' {
		p.pos++
		return v
	}
	for {
		p.ws()
		v.A = append(v.A, p.value())
		p.ws()
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return v
		default:
			p.fail("expected , or ]")
		}
	}
}

func (p *parser) object() *Value {
	p.pos++
	v := &Value{Kind: Object, O: map[string]*Value{}}
	p.ws()
	if p.peek() == '}' {
		p.pos++
		return v
	}
	for {
		p.ws()
		if p.peek() != '"' {
			p.fail("expected string key")
		}
		k := p.str()
		p.ws()
		p.expect(":")
		p.ws()
		if _, dup := v.O[k]; !dup {
			v.Keys = append(v.Keys, k)
		}
		v.O[k] = p.value()
		p.ws()
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return v
		default:
			p.fail("expected , or }")
		}
	}
}

func quote(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func (v *Value) Write(sb *strings.Builder, indent string, depth int) {
	nl := func(d int) {
		if indent != "" {
			sb.WriteByte('\n')
			sb.WriteString(strings.Repeat(indent, d))
		}
	}
	switch v.Kind {
	case Null:
		sb.WriteString("null")
	case Bool:
		sb.WriteString(strconv.FormatBool(v.B))
	case Number:
		sb.WriteString(strconv.FormatFloat(v.N, 'g', -1, 64))
	case String:
		sb.WriteString(quote(v.S))
	case Array:
		if len(v.A) == 0 {
			sb.WriteString("[]")
			return
		}
		sb.WriteByte('[')
		for i, e := range v.A {
			if i > 0 {
				sb.WriteByte(',')
			}
			nl(depth + 1)
			e.Write(sb, indent, depth+1)
		}
		nl(depth)
		sb.WriteByte(']')
	case Object:
		if len(v.Keys) == 0 {
			sb.WriteString("{}")
			return
		}
		sb.WriteByte('{')
		for i, k := range v.Keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			nl(depth + 1)
			sb.WriteString(quote(k))
			sb.WriteByte(':')
			if indent != "" {
				sb.WriteByte(' ')
			}
			v.O[k].Write(sb, indent, depth+1)
		}
		nl(depth)
		sb.WriteByte('}')
	}
}

func (v *Value) String() string {
	var sb strings.Builder
	v.Write(&sb, "", 0)
	return sb.String()
}

// Query walks a dotted path such as "users.1.name".
func (v *Value) Query(path string) (*Value, bool) {
	cur := v
	if path == "" {
		return cur, true
	}
	for _, part := range strings.Split(path, ".") {
		switch cur.Kind {
		case Object:
			n, ok := cur.O[part]
			if !ok {
				return nil, false
			}
			cur = n
		case Array:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(cur.A) {
				return nil, false
			}
			cur = cur.A[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func (v *Value) Depth() int {
	d := 0
	switch v.Kind {
	case Array:
		for _, e := range v.A {
			d = max(d, e.Depth())
		}
		return d + 1
	case Object:
		for _, e := range v.O {
			d = max(d, e.Depth())
		}
		return d + 1
	}
	return 0
}

func (v *Value) Count(counts map[Kind]int) {
	counts[v.Kind]++
	for _, e := range v.A {
		e.Count(counts)
	}
	for _, k := range v.Keys {
		v.O[k].Count(counts)
	}
}

const doc = `{
  "name": "marmot",
  "version": 1.5e0,
  "tags": ["go", "mlir", "compiler", "fast"],
  "stable": false,
  "owner": null,
  "users": [
    {"name": "ann", "age": 31, "langs": ["go", "c"]},
    {"name": "bob", "age": 27, "langs": []},
    {"name": "céline \"cc\"", "age": 45.5, "langs": ["rust"]}
  ],
  "limits": {"mem": 1024, "nested": {"deep": {"deeper": [1, [2, [3, [4]]]]}}},
  "empty": {}, "esc": "tab\there\nnewline \\ slash \/ 世"
}`

func main() {
	v, err := Parse(doc)
	if err != nil {
		fmt.Println("unexpected:", err)
		return
	}
	fmt.Println(v)
	var sb strings.Builder
	v.Write(&sb, "  ", 0)
	fmt.Println(len(sb.String()), strings.Count(sb.String(), "\n"))
	fmt.Println(sb.String()[:120])

	for _, q := range []string{"name", "users.1.name", "users.2.age", "users.5", "limits.nested.deep.deeper.1.1.1.0", "tags.3", "version", "owner", "esc", "users.0.langs", "nope.x", ""} {
		r, ok := v.Query(q)
		if !ok {
			fmt.Printf("%-40q missing\n", q)
			continue
		}
		s := r.String()
		if len(s) > 50 {
			s = s[:50] + "..."
		}
		fmt.Printf("%-40q %-7v %s\n", q, r.Kind, s)
	}
	counts := map[Kind]int{}
	v.Count(counts)
	kinds := make([]int, 0)
	for k := range counts {
		kinds = append(kinds, int(k))
	}
	sort.Ints(kinds)
	for _, k := range kinds {
		fmt.Printf("%s=%d ", Kind(k), counts[Kind(k)])
	}
	fmt.Println("depth", v.Depth())

	round, _ := Parse(v.String())
	fmt.Println("roundtrip equal:", round.String() == v.String())

	bad := []string{``, `{`, `[1,]`, `{"a" 1}`, `{"a":1,}`, `"abc`, `tru`, `01`, `-`, `1.`, `1e`, `[1 2]`, `{"a":1} x`, `"\x"`, "\"a\x01\"", `"\u12"`, `nul`, `[`, `{"k":[}`}
	for _, s := range bad {
		_, err := Parse(s)
		fmt.Printf("%-12q %v\n", s, err)
	}
	deep := strings.Repeat("[", 200) + strings.Repeat("]", 200)
	dv, err := Parse(deep)
	fmt.Println(err, dv.Depth())
	big := &strings.Builder{}
	big.WriteString("[")
	for i := 0; i < 3000; i++ {
		if i > 0 {
			big.WriteString(",")
		}
		fmt.Fprintf(big, `{"id":%d,"sq":%d,"name":"item%d"}`, i, i*i, i)
	}
	big.WriteString("]")
	bv, err := Parse(big.String())
	last, _ := bv.Query("2999.sq")
	fmt.Println(err, len(bv.A), last, len(bv.String()) == big.Len())
}

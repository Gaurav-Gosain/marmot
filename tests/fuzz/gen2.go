package main

import (
	"fmt"
	"strings"
)

// Second generator: constructs that loop optimization and library specialization transform.
// Everything is emitted inside a scoped block so names never collide.

var strPool = []string{`""`, `"a"`, `"hello"`, `"q\"uote"`, `"tab\there"`, `"é世"`, `"<&>"`, `"line\nbreak"`, `"\\back"`, `"x y z"`}

type jfield struct {
	name, typ, tag string
}

type jtype struct {
	name   string
	fields []jfield
}

// genDecls creates package level types used by ext2 statements.
func (g *gen) genDecls() {
	nj := 1 + g.r.Intn(2)
	kinds := []string{"int", "int8", "uint16", "int64", "string", "float64", "bool", "[]int", "map[string]int", "[]string", "uint8"}
	for i := 0; i < nj; i++ {
		jt := jtype{name: fmt.Sprintf("J%d", i)}
		nf := 2 + g.r.Intn(5)
		for f := 0; f < nf; f++ {
			t := kinds[g.r.Intn(len(kinds))]
			tag := fmt.Sprintf(`json:"f%d"`, f)
			switch g.r.Intn(5) {
			case 0:
				tag = fmt.Sprintf(`json:"f%d,omitempty"`, f)
			case 1:
				tag = fmt.Sprintf(`json:"n%d_%s"`, f, strings.NewReplacer("[]", "s", "map[string]", "m").Replace(t))
			}
			jt.fields = append(jt.fields, jfield{fmt.Sprintf("F%d", f), t, tag})
		}
		g.jtypes = append(g.jtypes, jt)
		var sb strings.Builder
		fmt.Fprintf(&sb, "type %s struct {\n", jt.name)
		for _, f := range jt.fields {
			fmt.Fprintf(&sb, "\t%s %s `%s`\n", f.name, f.typ, f.tag)
		}
		sb.WriteString("}\n")
		g.decls = append(g.decls, sb.String())
	}
	g.decls = append(g.decls, "type pt struct {\n\ta int\n\tb float64\n\tc string\n}\n")
	g.decls = append(g.decls, "type mkey struct {\n\ta int8\n\tb string\n\tc [2]uint8\n}\n")
	g.decls = append(g.decls, "type smallKey [3]int16\n")
}

func (g *gen) jvalue(t string) string {
	switch t {
	case "int", "int8", "int64", "uint16", "uint8":
		return fmt.Sprintf("%s(%d)", t, g.r.Intn(120)-20*boolToInt(!strings.HasPrefix(t, "u")))
	case "string":
		return strPool[g.r.Intn(len(strPool))]
	case "float64":
		return fmt.Sprintf("float64(%d)/8", g.r.Intn(2000)-1000)
	case "bool":
		return []string{"true", "false"}[g.r.Intn(2)]
	case "[]int":
		return fmt.Sprintf("[]int{%d, %d, %d}", g.r.Intn(50), g.r.Intn(50), g.r.Intn(50))
	case "[]string":
		return fmt.Sprintf("[]string{%s, %s}", strPool[g.r.Intn(len(strPool))], strPool[g.r.Intn(len(strPool))])
	case "map[string]int":
		return fmt.Sprintf("map[string]int{%s: %d, \"zz\": %d}", strPool[g.r.Intn(len(strPool))], g.r.Intn(9), g.r.Intn(9))
	}
	return "nil"
}

func (g *gen) ext2Stmt() {
	i := fmt.Sprintf("%d", len(g.lines))
	switch g.r.Intn(8) {
	case 0: // nested counted loops over a slice, with reductions
		n := 4 + g.r.Intn(20)
		v := g.anyVar()
		g.emit("{")
		g.emit("\txs := make([]int, %d)", n)
		g.emit("\tfor i := range xs {")
		g.emit("\t\txs[i] = i*%d - %d", 1+g.r.Intn(9), g.r.Intn(30))
		g.emit("\t}")
		g.emit("\tfor i := 0; i < len(xs); i++ {")
		g.emit("\t\tfor j := 0; j < %d; j++ {", 1+g.r.Intn(4))
		g.emit("\t\t\txs[i] += int(%s)*(j+1) ^ i", v)
		g.emit("\t\t}")
		g.emit("\t}")
		g.emit("\tacc, mn := 0, xs[0]")
		g.emit("\tfor _, x := range xs {")
		g.emit("\t\tacc += x * x")
		g.emit("\t\tmn = min(mn, x)")
		g.emit("\t}")
		g.emit("\tprintln(acc, mn, xs[len(xs)-1])")
		g.emit("}")
	case 1: // aliasing slices
		n := 8 + g.r.Intn(24)
		k := 1 + g.r.Intn(4)
		g.emit("{")
		g.emit("\txs := make([]int, %d)", n)
		g.emit("\tfor i := range xs {")
		g.emit("\t\txs[i] = i + 1")
		g.emit("\t}")
		g.emit("\tys := xs[%d:]", k)
		g.emit("\tfor i := range ys {")
		g.emit("\t\tys[i] += xs[i]")
		g.emit("\t}")
		g.emit("\tcopy(xs[%d:], xs)", k)
		g.emit("\tfor i := %d; i < len(xs); i++ {", k)
		g.emit("\t\txs[i] += xs[i-%d]", k)
		g.emit("\t}")
		g.emit("\ts := 0")
		g.emit("\tfor i, x := range xs {")
		g.emit("\t\ts += x * (i + 1)")
		g.emit("\t}")
		g.emit("\tprintln(s, xs[0], xs[len(xs)-1])")
		g.emit("}")
	case 2: // slice of structs
		n := 3 + g.r.Intn(10)
		g.emit("{")
		g.emit("\tps := make([]pt, %d)", n)
		g.emit("\tfor i := range ps {")
		g.emit("\t\tps[i] = pt{i * %d, float64(i) / 4, %s}", 1+g.r.Intn(5), strPool[g.r.Intn(len(strPool))])
		g.emit("\t}")
		g.emit("\tfor i := range ps {")
		g.emit("\t\tps[i].a += int(%s)", g.anyVar())
		g.emit("\t\tps[i].b *= 1.5")
		g.emit("\t\tps[i].c += \"!\"")
		g.emit("\t}")
		g.emit("\tt, f := 0, 0.0")
		g.emit("\tfor _, p := range ps {")
		g.emit("\t\tt += p.a + len(p.c)")
		g.emit("\t\tf += p.b")
		g.emit("\t}")
		g.emit("\tfmt.Println(t, f, ps[0], ps[len(ps)-1])")
		g.emit("}")
	case 3: // fmt verbs
		g.emit("{")
		for k := 0; k < 3; k++ {
			g.fmtLine()
		}
		g.emit("}")
	case 4: // json round trip
		jt := g.jtypes[g.r.Intn(len(g.jtypes))]
		g.emit("{")
		g.emit("\tin := %s{", jt.name)
		for _, f := range jt.fields {
			if g.r.Intn(4) == 0 {
				continue
			}
			g.emit("\t\t%s: %s,", f.name, g.jvalue(f.typ))
		}
		g.emit("\t}")
		g.emit("\tb, err := json.Marshal(in)")
		g.emit("\tfmt.Println(string(b), err)")
		g.emit("\tvar out %s", jt.name)
		g.emit("\terr = json.Unmarshal(b, &out)")
		g.emit("\tb2, _ := json.Marshal(out)")
		g.emit("\tfmt.Println(string(b2) == string(b), err)")
		g.emit("\tb3, _ := json.MarshalIndent([]%s{in, out}, \"\", \" \")", jt.name)
		g.emit("\tfmt.Println(len(b3))")
		g.emit("}")
	case 5: // sort with custom comparison, ties included
		n := 5 + g.r.Intn(40)
		m := 2 + g.r.Intn(6)
		g.emit("{")
		g.emit("\tps := make([]pt, %d)", n)
		g.emit("\tfor i := range ps {")
		g.emit("\t\tps[i] = pt{(i*%d + int(%s)) %% %d, float64(i), fmt.Sprint(i)}", 3+g.r.Intn(7), g.anyVar(), m)
		g.emit("\t}")
		switch g.r.Intn(3) {
		case 0:
			g.emit("\tsort.Slice(ps, func(i, j int) bool { return ps[i].a < ps[j].a })")
		case 1:
			g.emit("\tsort.SliceStable(ps, func(i, j int) bool { return ps[i].a > ps[j].a })")
		default:
			g.emit("\tslices.SortFunc(ps, func(x, y pt) int { return cmp.Compare(x.a%%%d, y.a%%%d) })", m, m)
		}
		g.emit("\tvar sb strings.Builder")
		g.emit("\tfor _, p := range ps {")
		g.emit("\t\tsb.WriteString(p.c + \",\")")
		g.emit("\t}")
		g.emit("\tprintln(sb.String())")
		g.emit("}")
	case 6: // maps with generated key types
		g.emit("{")
		g.emit("\tm := map[mkey]int{}")
		g.emit("\tfor i := 0; i < %d; i++ {", 10+g.r.Intn(40))
		g.emit("\t\tk := mkey{int8(i %% %d), []string{\"a\", \"b\", \"c\"}[i%%3], [2]uint8{uint8(i %% 2), 1}}", 2+g.r.Intn(5))
		g.emit("\t\tm[k] += i")
		g.emit("\t}")
		g.emit("\tsm := map[smallKey]string{}")
		g.emit("\tfor i := 0; i < 12; i++ {")
		g.emit("\t\tsm[smallKey{int16(i %% 3), int16(i %% 2), 7}] += \"x\"")
		g.emit("\t}")
		g.emit("\tvar lines []string")
		g.emit("\tfor k, v := range m {")
		g.emit("\t\tlines = append(lines, fmt.Sprint(k, v))")
		g.emit("\t}")
		g.emit("\tfor k, v := range sm {")
		g.emit("\t\tlines = append(lines, fmt.Sprint(k, v))")
		g.emit("\t}")
		g.emit("\tsort.Strings(lines)")
		g.emit("\tfmt.Println(len(m), len(sm), strings.Join(lines, \";\"))")
		g.emit("\tdelete(m, mkey{0, \"a\", [2]uint8{0, 1}})")
		g.emit("\tfmt.Println(len(m))")
		g.emit("}")
	default: // float reductions with explicit rounding
		n := 10 + g.r.Intn(200)
		g.emit("{")
		g.emit("\tfs := make([]float64, %d)", n)
		g.emit("\tfor i := range fs {")
		g.emit("\t\tfs[i] = 1 / float64(i+%d)", 1+g.r.Intn(5))
		g.emit("\t}")
		g.emit("\ts, p := 0.0, 1.0")
		g.emit("\tfor _, x := range fs {")
		g.emit("\t\ts += x")
		g.emit("\t\tp = float64(p * (1 + x))")
		g.emit("\t}")
		g.emit("\tprintln(s, p)")
		g.emit("}")
	}
	_ = i
}

var intVerbs = []string{"%d", "%5d", "%-5d|", "%05d", "%+d", "% d", "%x", "%X", "%#x", "%o", "%#o", "%b", "%08b", "%c", "%q", "%U", "%v", "%3d", "%.3d", "%+.2d", "%x"}
var floatVerbs = []string{"%f", "%.2f", "%8.3f", "%-8.1f|", "%e", "%E", "%g", "%G", "%+.3e", "%08.2f", "%.0f", "%6.1f", "%v", "%.3g", "%10.4g", "%+g", "% .1f"}
var strVerbs = []string{"%s", "%q", "%10s", "%-10s|", "%.3s", "%x", "%X", "%v", "%+q", "%#q", "% x", "%8.2s", "%5s"}

func (g *gen) fmtLine() {
	switch g.r.Intn(5) {
	case 0, 1:
		v := g.vars[g.r.Intn(len(g.vars))]
		verb := intVerbs[g.r.Intn(len(intVerbs))]
		arg := v.name
		if strings.Contains(verb, "c") || strings.Contains(verb, "q") || strings.Contains(verb, "U") {
			arg = "rune(" + v.name + ")"
		}
		g.emit(`fmt.Printf("%s\n", %s)`, verb, arg)
	case 2:
		verb := floatVerbs[g.r.Intn(len(floatVerbs))]
		g.emit(`fmt.Printf("%s\n", f)`, verb)
		g.emit(`fmt.Printf("%s\n", float32(f))`, verb)
	case 3:
		verb := strVerbs[g.r.Intn(len(strVerbs))]
		g.emit(`fmt.Printf("%s\n", s)`, verb)
		g.emit(`fmt.Printf("%s\n", []byte(s))`, verb)
	default:
		g.emit(`fmt.Printf("%%v|%%T|%%t|%%5t|%%v\n", %s, %s, %s != 0, %s == 0, []int{int(%s), 2})`, g.anyVar(), g.anyVar(), g.anyVar(), g.anyVar(), g.anyVar())
	}
}

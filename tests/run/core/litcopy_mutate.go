// Stores into a composite literal right after it is built (codegen
// litcopy.go with MARMOT_LITCOPY=1): constant and run-time stores into
// fields, nested fields, elements and whole nested values must all win
// over the literal's own values, for pointer and value literals, in loops,
// next to maps, and whether or not the object escapes. The first literal
// image pass dropped cfg.Notif.Bell = "toast" after Notif: Notif{...}.
package main

import "fmt"

type Notif struct {
	RateLimitMS int
	BellRate    int
	Bell        string
	Style       string
}

type Config struct {
	Name  string
	Notif Notif
	Keys  map[string][]string
	Grid  [3][2]int
	Tags  [4]string
}

const BellToast = "toast"

var sink *Config

//go:noinline
func ptrLit() *Config {
	cfg := &Config{Name: "x", Notif: Notif{RateLimitMS: 5, BellRate: 7}, Keys: map[string][]string{"a": {"b"}}}
	cfg.Notif.Bell = BellToast
	cfg.Notif.Style = "full"
	return cfg
}

//go:noinline
func overwriteLit() *Config {
	cfg := &Config{Name: "x", Notif: Notif{RateLimitMS: 5, BellRate: 7, Bell: "init", Style: "s"}}
	cfg.Notif.Bell = "after"
	cfg.Notif.RateLimitMS = 9
	cfg.Name = "y"
	return cfg
}

//go:noinline
func wholeAfterParts() Config {
	c := Config{Name: "n", Notif: Notif{1, 2, "a", "b"}, Tags: [4]string{"t0", "t1", "t2", "t3"}}
	c.Notif.Bell = "part"
	c.Notif = Notif{RateLimitMS: 3, Style: "whole"} // the whole value after a part
	c.Tags[2] = "T2"
	return c
}

//go:noinline
func nestedArrays(k int) Config {
	c := Config{Grid: [3][2]int{{1, 2}, {3, 4}, {5, 6}}, Tags: [4]string{"a", "b", "c", "d"}}
	c.Grid[1][0] = 30
	c.Grid[2] = [2]int{50, 60}
	c.Tags[k] = "run"
	c.Tags[3] = "const"
	return c
}

//go:noinline
func valueLitNoEscape() int {
	c := Config{Notif: Notif{RateLimitMS: 1, BellRate: 2, Bell: "b", Style: "s"}, Grid: [3][2]int{{1, 1}, {1, 1}, {1, 1}}}
	c.Notif.BellRate = 20
	c.Grid[0][1] = 10
	return c.Notif.BellRate + c.Grid[0][1] + len(c.Notif.Bell)
}

//go:noinline
func escaping(s string) {
	c := &Config{Name: "e", Notif: Notif{RateLimitMS: 1, BellRate: 2}, Tags: [4]string{"1", "2", "3", "4"}}
	c.Notif.Style = s
	c.Notif.Bell = "escaped"
	sink = c
}

//go:noinline
func inLoop() []Config {
	var out []Config
	for i := 0; i < 3; i++ {
		c := Config{Name: "l", Notif: Notif{RateLimitMS: 1, BellRate: 2, Bell: "x", Style: "y"}}
		c.Notif.Bell = fmt.Sprint("i", i)
		if i == 1 {
			c.Notif.Style = "one"
		}
		c.Grid[i][1] = i
		out = append(out, c)
	}
	return out
}

//go:noinline
func mapAfter() map[string]Notif {
	m := map[string]Notif{
		"a": {1, 2, "a", "a"}, "b": {3, 4, "b", "b"}, "c": {5, 6, "c", "c"}, "d": {7, 8, "d", "d"},
		"e": {9, 10, "e", "e"}, "f": {11, 12, "f", "f"}, "g": {13, 14, "g", "g"}, "h": {15, 16, "h", "h"},
	}
	n := m["c"]
	n.Bell = "changed"
	m["c"] = n
	m["a"] = Notif{Style: "replaced"}
	delete(m, "h")
	return m
}

var global = Config{Name: "g", Notif: Notif{RateLimitMS: 1, BellRate: 2, Bell: "gb", Style: "gs"}}

func init() {
	global.Notif.Bell = "from init"
}

func main() {
	d := ptrLit()
	fmt.Printf("%q %q %d %d %v\n", d.Notif.Bell, d.Notif.Style, d.Notif.BellRate, d.Notif.RateLimitMS, d.Keys)
	o := overwriteLit()
	fmt.Printf("%q %q %d %d %q\n", o.Notif.Bell, o.Notif.Style, o.Notif.RateLimitMS, o.Notif.BellRate, o.Name)
	fmt.Printf("%+v\n", wholeAfterParts())
	for k := 0; k < 2; k++ {
		c := nestedArrays(k)
		fmt.Println(c.Grid, c.Tags)
	}
	fmt.Println(valueLitNoEscape())
	escaping("run time")
	fmt.Printf("%+v\n", *sink)
	for _, c := range inLoop() {
		fmt.Println(c.Notif, c.Grid)
	}
	m := mapAfter()
	fmt.Println(len(m), m["a"], m["c"], m["h"], m["g"])
	fmt.Printf("%+v\n", global.Notif)
	// The same function twice: each call gets a fresh object.
	a, b := ptrLit(), ptrLit()
	a.Notif.Bell = "mutated"
	fmt.Println(a.Notif.Bell, b.Notif.Bell, a != b)
}

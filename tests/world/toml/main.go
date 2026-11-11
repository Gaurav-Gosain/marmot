package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type Server struct {
	Host string   `toml:"host"`
	Port int      `toml:"port"`
	Tags []string `toml:"tags"`
}

type Config struct {
	Title   string            `toml:"title"`
	Debug   bool              `toml:"debug"`
	Ratio   float64           `toml:"ratio"`
	Servers map[string]Server `toml:"servers"`
	Limits  struct {
		Max   int64 `toml:"max"`
		Names []string
	} `toml:"limits"`
	Matrix [][]int `toml:"matrix"`
}

const doc = `
title = "TOML éxample"
debug = true
ratio = 1.5e2
matrix = [[1, 2], [3, 4, 5]]

[servers.alpha]
host = "10.0.0.1"
port = 8080
tags = ["a", "b"]

[servers.beta]
host = "10.0.0.2"
port = 8081
tags = []

[limits]
max = 0xff
Names = ['lit', """multi
line"""]
`

func main() {
	var c Config
	md, err := toml.Decode(doc, &c)
	fmt.Println(err, c.Title, c.Debug, c.Ratio, c.Limits.Max, c.Limits.Names, c.Matrix)
	var names []string
	for k := range c.Servers {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Println(n, c.Servers[n])
	}
	keys := md.Keys()
	fmt.Println(len(keys), md.IsDefined("servers", "alpha", "port"), md.IsDefined("nope"), len(md.Undecoded()))
	var sb strings.Builder
	err = toml.NewEncoder(&sb).Encode(c)
	fmt.Println(err)
	fmt.Print(sb.String())
	var m map[string]any
	_, err = toml.Decode("a = 1\nb = [1, 2.5]\n[c]\nd = 1979-05-27T07:32:00Z\n", &m)
	fmt.Println(err, m["a"], m["b"], m["c"])
	for _, bad := range []string{"a = ", "a = 1\na = 2", "[x\n", "a = 'unterminated", "= 3"} {
		var x map[string]any
		_, err := toml.Decode(bad, &x)
		fmt.Printf("%q -> %v\n", bad, err)
	}
}

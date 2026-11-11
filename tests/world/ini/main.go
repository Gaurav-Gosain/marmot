package main

import (
	"bytes"
	"fmt"
	"sort"

	"gopkg.in/ini.v1"
)

const src = `
; comment
app_name = demo
debug = true
[server]
host = localhost
port = 8080
timeout = 1.5
paths = /a,/b ,/c
motd = "quoted # not a comment" ; trailing
multi = """line one
line two"""
[server.tls]
enabled = yes
cert = /etc/${app_name}.pem
[empty]
[list]
key[] = a
key[] = b
`

func main() {
	f, err := ini.Load([]byte(src))
	fmt.Println(err)
	names := f.SectionStrings()
	fmt.Println(names)
	fmt.Println(f.Section("").Key("app_name").String(), f.Section("").Key("debug").MustBool())
	s := f.Section("server")
	port, _ := s.Key("port").Int()
	to, _ := s.Key("timeout").Float64()
	fmt.Println(s.Key("host").Value(), port, to, s.Key("paths").Strings(","), s.Key("motd").String(), s.Key("multi").String())
	fmt.Println(f.Section("server.tls").Key("enabled").MustBool(), f.Section("server.tls").Key("cert").String())
	fmt.Println(s.KeyStrings(), s.HasKey("nope"), f.Section("nosuch").Key("x").MustInt(7))
	keys := s.Keys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name() < keys[j].Name() })
	fmt.Println(len(keys), keys[0].Name())
	type Cfg struct {
		Name  string `ini:"app_name"`
		Debug bool
		Srv   struct {
			Host string
			Port int
		} `ini:"server"`
	}
	var c Cfg
	fmt.Println(f.MapTo(&c), c.Name, c.Debug, c.Srv.Host, c.Srv.Port)
	f.Section("new").NewKey("k", "v")
	var buf bytes.Buffer
	f.WriteTo(&buf)
	fmt.Print(buf.String())
	_, err = ini.Load([]byte("[unclosed\nkey=1"))
	fmt.Println(err)
}

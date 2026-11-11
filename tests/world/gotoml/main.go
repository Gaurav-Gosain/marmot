package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Cfg struct {
	Name  string
	Port  int
	Rate  float64
	On    bool
	Tags  []string
	When  time.Time
	Dur   time.Duration
	Sub   struct{ A, B int }
	Items []map[string]any
	Opt   *string `toml:"opt,omitempty"`
}

const doc = `
Name = "svc"
Port = 8080
Rate = 0.25
On = true
Tags = ["a", "b"]
When = 2024-02-29T12:30:00Z
Dur = "1h30m"
[Sub]
A = 1
B = 2
[[Items]]
k = "v1"
n = 1
[[Items]]
k = "v2"
`

func main() {
	var c Cfg
	err := toml.Unmarshal([]byte(doc), &c)
	fmt.Println(err, c.Name, c.Port, c.Rate, c.On, c.Tags, c.When, c.Dur, c.Sub, c.Items)
	out, err := toml.Marshal(c)
	fmt.Println(err)
	fmt.Print(string(out))
	var m map[string]any
	fmt.Println(toml.Unmarshal([]byte("a = 1\nb = {x = 1, y = [1, 2]}\nc = 1979-05-27\nd = 07:32:00\n"), &m), m)
	for _, bad := range []string{"a = ", "a = 1\na = 2", "[x", "a = 'x", "a = 0x", "a = [1, 'x']\nb"} {
		var v map[string]any
		err := toml.Unmarshal([]byte(bad), &v)
		fmt.Printf("%q -> %v\n", bad, err)
		if de, ok := err.(*toml.DecodeError); ok {
			r, c := de.Position()
			fmt.Println(r, c)
		}
	}
	var strict struct{ A int }
	d := toml.NewDecoder(strings.NewReader("A = 1\nB = 2\n"))
	d.DisallowUnknownFields()
	fmt.Println(d.Decode(&strict))
}

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Level int

func (l Level) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.Repeat("*", int(l)))
}

func (l *Level) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*l = Level(len(s))
	return nil
}

type Color struct{ R, G, B uint8 }

func (c Color) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)), nil
}

type Wrap struct {
	L Level         `json:"level"`
	C Color         `json:"color"`
	M map[Color]int `json:"m"`
	T time.Time     `json:"t"`
	D time.Duration `json:"d"`
}

func main() {
	w := Wrap{3, Color{255, 0, 128}, map[Color]int{{1, 2, 3}: 5}, time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), time.Second}
	b, err := json.Marshal(w)
	fmt.Println(string(b), err)
	var back Level
	fmt.Println(json.Unmarshal([]byte(`"*****"`), &back), back)
	var t time.Time
	fmt.Println(json.Unmarshal([]byte(`"2024-05-06T07:08:09Z"`), &t), t.Year())
}

// fmtlog: structured log lines built with Sprintf and %v of structs, as a logger would.
package main

import (
	"fmt"
	"hash/fnv"
)

const lines = 1500000

type event struct {
	ID    int
	Kind  string
	OK    bool
	Delay float64
}

func main() {
	h := fnv.New64a()
	levels := []string{"INFO", "WARN", "ERROR", "DEBUG"}
	for i := 0; i < lines; i++ {
		ev := event{i, levels[i&3], i%5 != 0, float64(i%1000) / 8}
		msg := fmt.Sprintf("ts=%08d level=%-5s id=%d ok=%t delay=%.3f ev=%v", i, ev.Kind, ev.ID, ev.OK, ev.Delay, ev)
		h.Write([]byte(msg))
		h.Write([]byte{'\n'})
	}
	fmt.Println(h.Sum64())
}

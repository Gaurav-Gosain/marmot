package specialize

import "testing"

const builderProgram = `package main

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type T struct{ sb strings.Builder }

func main() {
	var sb strings.Builder
	var bb bytes.Buffer
	caps := []int{}
	for i := -3; i < 300; i++ {
		sb.WriteString(strconv.Itoa(i * 7919))
		sb.WriteString(strconv.FormatInt(int64(i), 16))
		sb.WriteString(strconv.FormatInt(-42, 36))
		sb.WriteString(strconv.FormatUint(uint64(i*i), 2))
		sb.WriteString(strconv.FormatFloat(float64(i)/3, 'f', 2, 64))
		sb.WriteString(strconv.FormatFloat(math.Inf(1), 'g', -1, 32))
		sb.WriteString(strconv.FormatBool(i%2 == 0))
		sb.WriteString(strconv.Quote("q\"\n" + strconv.Itoa(i)))
		bb.WriteString(strconv.Itoa(i))
		bb.WriteString(strconv.FormatFloat(1e21, 'e', 3, 64))
		caps = append(caps, sb.Cap(), bb.Cap())
	}
	fmt.Println(sb.Len(), sb.String()[:300], bb.String()[:200], caps)
	var t T
	t.sb.WriteString(strconv.Itoa(5))
	n := 0
	f := func() int { n++; sb.WriteString("!"); return n }
	sb.WriteString(strconv.Itoa(f()))
	fmt.Println(t.sb.String(), sb.String()[sb.Len()-10:])
	var p *strings.Builder
	defer func() { fmt.Println("recovered", recover() != nil, n) }()
	p.WriteString(strconv.Itoa(f()))
}
`

func TestBuilderDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, builderProgram, 12)
}

package specialize

import "testing"

const foldProgram = `package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Name string

func main() {
	fmt.Println(strconv.Itoa(-0), strconv.Itoa(1<<40), strconv.Quote("a\tb\x00é\xff"), strconv.FormatInt(-255, 16), strconv.FormatInt(7, 36), strconv.FormatBool(true))
	fmt.Println(strings.Repeat("ab", 3), strings.ToUpper("héllo ß"), strings.ToLower("ÀÉ"), strings.TrimSpace("\t x \n"), strings.ReplaceAll("aaa", "a", "bc"))
	fmt.Println(strings.Contains("abc", ""), strings.Index("chicken", "ken"), strings.LastIndex("go gopher", "go"), strings.Count("cheese", "e"), strings.Count("five", ""))
	fmt.Println(strings.HasPrefix("golang", "go"), strings.HasSuffix("golang", "ng"), strings.EqualFold("Go", "GO"), strings.Trim("xxaxx", "x"), strings.TrimPrefix("ab", "a"), strings.TrimSuffix("ab", "b"))
	fmt.Println(math.Sqrt(2), math.Sqrt(-1), math.Abs(-3.5), math.Floor(-1.5), math.Ceil(1.2), math.Trunc(-0.5), math.Sqrt(1e308))
	const n Name = "named"
	fmt.Println(strings.ToUpper(string(n)))
	x := 10
	defer func() { fmt.Println("recovered:", recover()) }()
	fmt.Println(x / strings.Index("abc", "a"))
}

func init() {
	defer func() { fmt.Println("init recovered:", recover() != nil) }()
	fmt.Println(strconv.FormatInt(5, 99))
}

func init() {
	defer func() { fmt.Println("init2 recovered:", recover() != nil) }()
	fmt.Println(strings.Repeat("x", -1))
}
`

func TestFoldDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, foldProgram, 20)
}

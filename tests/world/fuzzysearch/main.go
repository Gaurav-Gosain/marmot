package main

import (
	"fmt"
	"sort"

	"github.com/lithammer/fuzzysearch/fuzzy"
)

func main() {
	words := []string{"cartwheel", "foobar", "wheel", "baz", "Cartwheel", "héllo wörld", "日本語テキスト", "a_very_long_identifier_name"}
	for _, q := range []string{"car", "whl", "ba", "hello", "テキ", "", "zzz", "vlin", "CAR"} {
		fmt.Println(q, fuzzy.Find(q, words), fuzzy.FindFold(q, words), fuzzy.Match(q, "cartwheel"), fuzzy.MatchFold(q, "Cartwheel"))
	}
	r := fuzzy.RankFind("whl", words)
	sort.Sort(r)
	fmt.Println(r)
	fmt.Println(fuzzy.RankMatch("kitten", "sitting"), fuzzy.RankMatch("abc", "xyz"), fuzzy.RankMatchFold("ABC", "abcdef"), fuzzy.RankMatchNormalized("cafe", "café"))
	fmt.Println(fuzzy.MatchNormalized("cafe", "Café"), fuzzy.FindNormalized("uber", []string{"über", "uber", "x"}))
}

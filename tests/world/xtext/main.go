package main

import (
	"fmt"

	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"
)

func main() {
	s := "Crème Brûlée ǆ ﬁ"
	fmt.Println(norm.NFC.String(s), len(norm.NFC.String(s)), len(norm.NFD.String(s)), len(norm.NFKC.String(s)), len(norm.NFKD.String(s)))
	fmt.Println(norm.NFC.IsNormalString("é"), norm.NFC.IsNormalString("é"), norm.NFD.String("é") == "é")
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, "Crème Brûlée Ångström")
	fmt.Println(out, err)
	fmt.Println(cases.Upper(language.English).String("hello wörld ß"), cases.Lower(language.English).String("HELLO İ"), cases.Title(language.English).String("the quick brown-fox o'neil"))
	fmt.Println(cases.Upper(language.Turkish).String("iı"), cases.Lower(language.Turkish).String("Iİ"), cases.Fold().String("Straße"))
	fmt.Println(width.Narrow.String("ＡＢＣ１２３ ｱ"), width.Widen.String("ABC123"), width.Fold.String("ＡＢＣ"))
	p, _ := width.LookupString("世")
	fmt.Println(p.Kind(), p.Wide(), width.LookupRune('A').Kind())
	var it norm.Iter
	it.InitString(norm.NFD, "éa")
	for !it.Done() {
		fmt.Printf("%q ", it.Next())
	}
	fmt.Println()
	tag, _ := language.Parse("en-US-u-co-phonebk")
	b, conf := language.MatchStrings(language.NewMatcher([]language.Tag{language.French, language.German, language.English}), "de-AT, en;q=0.5")
	fmt.Println(tag, b, conf)
}

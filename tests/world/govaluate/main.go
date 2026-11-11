package main

import (
	"fmt"

	"github.com/Knetic/govaluate"
)

func main() {
	params := map[string]any{"x": 10, "y": 3.5, "name": "marmot", "flag": true}
	for _, e := range []string{
		"1 + 2 * 3", "(1 + 2) * 3", "x * y", "x / 4", "x % 3", "2 ** 10", "-x + 5", "x > 5 && y < 4", "x > 5 || false", "!flag", "flag ? 1 : 2",
		"name + ' rocks'", "name == 'marmot'", "'a' < 'b'", "x in (1, 5, 10)", "x in (1, 2)", "0x10 + 1", "1.5e2", "x & 3", "x | 5", "x ^ 3", "~x", "x << 2", "x >> 1",
		"(x > 5) ? 'big' : 'small'", "x ?? 7", "'abc' =~ 'a.c'", "'abc' !~ 'x.*'", "1 / 0", "1 +", "foo(1)", "x +* y", "unknownvar + 1", "true && 1",
	} {
		ex, err := govaluate.NewEvaluableExpression(e)
		if err != nil {
			fmt.Printf("%-28s parse error: %v\n", e, err)
			continue
		}
		r, err := ex.Evaluate(params)
		fmt.Printf("%-28s %v (%T) err=%v\n", e, r, r, err)
	}
	fns := map[string]govaluate.ExpressionFunction{
		"double": func(a ...any) (any, error) { return a[0].(float64) * 2, nil },
		"strlen": func(a ...any) (any, error) { return float64(len(a[0].(string))), nil },
	}
	ex, _ := govaluate.NewEvaluableExpressionWithFunctions("double(x) + strlen(name)", fns)
	r, err := ex.Evaluate(params)
	fmt.Println(r, err, ex.Vars())
	ex2, _ := govaluate.NewEvaluableExpression("[start] + 1")
	r, _ = ex2.Evaluate(map[string]any{"start": 41})
	fmt.Println(r, ex2.String())
}

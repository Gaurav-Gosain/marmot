package main

import "os"

func main() {
	println(len(os.Args) >= 1, len(os.Args[0]) > 0)
	os.Setenv("MARMOT_A", "1")
	os.Setenv("MARMOT_B", "")
	println(os.Getenv("MARMOT_A"), os.Getenv("MARMOT_B") == "", os.Getenv("MARMOT_NOPE_ZZ") == "")
	_, ok := os.LookupEnv("MARMOT_B")
	_, ok2 := os.LookupEnv("MARMOT_NOPE_ZZ")
	println(ok, ok2)
	os.Unsetenv("MARMOT_A")
	_, ok = os.LookupEnv("MARMOT_A")
	println(ok)
	println(os.ExpandEnv("x${MARMOT_B}y$MARMOT_NOPE_ZZ."), os.Expand("$a-$b", func(k string) string { return "<" + k + ">" }))
	found := false
	for _, e := range os.Environ() {
		if e == "MARMOT_B=" {
			found = true
		}
	}
	println(found)
}

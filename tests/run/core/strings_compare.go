package main

func main() {
	a, b := "apple", "apricot"
	println(a < b, a > b, a <= a, a >= b)
	println("abc" < "abd", "abc" < "ab", "ab" < "abc", "Z" < "a")
	println("\xff" > "a", "\x00" < "a", "a\x00" > "a")
	x := "same"
	y := "sa" + "me"
	println(x == y)
	var z string
	println(z < "a", z == "", z >= "")
	long1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab"
	long2 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaac"
	println(long1 < long2, long1 == long2, long1[:60] == long2[:60])
}

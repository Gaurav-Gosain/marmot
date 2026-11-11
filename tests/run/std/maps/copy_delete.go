package main

import "maps"

func main() {
	dst := map[int]string{1: "a"}
	src := map[int]string{1: "A", 2: "B"}
	maps.Copy(dst, src)
	println(len(dst), dst[1], dst[2])
	maps.DeleteFunc(dst, func(k int, v string) bool { return k == 1 })
	println(len(dst), dst[2])
	maps.DeleteFunc(dst, func(int, string) bool { return true })
	println(len(dst))
}

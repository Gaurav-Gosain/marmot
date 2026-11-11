package main

import "maps"

func main() {
	prices := map[string]float64{"a": 1.5, "b": 2.25, "c": 3}
	sum := 0.0
	for v := range maps.Values(prices) {
		sum += v
	}
	println(sum)
	keys := 0
	for range maps.Keys(prices) {
		keys++
	}
	println(keys)
	for k, v := range maps.All(map[int]int{}) {
		println(k, v)
	}
}

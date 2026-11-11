package main

import "time"

func main() {
	for m := time.January; m <= time.December; m++ {
		print(m.String()[:3], " ")
	}
	println()
	for d := time.Sunday; d <= time.Saturday; d++ {
		print(d.String()[:2], " ")
	}
	println()
	println(time.Month(13).String(), time.Weekday(8).String())
	days := 0
	for m := 1; m <= 12; m++ {
		days += time.Date(2024, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	}
	println(days)
	for y := 1896; y <= 2104; y += 4 {
		if time.Date(y, 3, 0, 0, 0, 0, 0, time.UTC).Day() == 29 {
			print(y, " ")
		}
	}
	println()
}

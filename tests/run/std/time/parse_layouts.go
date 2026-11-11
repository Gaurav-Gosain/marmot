package main

import "time"

func main() {
	for _, c := range [][2]string{
		{"2006-01-02", "2024-03-15"},
		{time.RFC3339, "2024-03-15T10:20:30Z"},
		{time.RFC3339, "2024-03-15T10:20:30+02:00"},
		{"2006-01-02 15:04", "2024-13-01 10:00"},
		{"Jan 2, 2006", "Mar 5, 2021"},
		{"15:04:05", "25:00:00"},
		{time.RFC3339Nano, "2024-03-15T10:20:30.123456789Z"},
		{"2006-01-02", "2024-02-30"},
		{"2006-01-02", "garbage"},
	} {
		t, err := time.Parse(c[0], c[1])
		if err != nil {
			println(c[1], "error")
			continue
		}
		println(c[1], t.UTC().Format(time.RFC3339Nano), t.Unix())
	}
}

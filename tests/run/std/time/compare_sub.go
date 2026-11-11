package main

import "time"

func main() {
	a := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	b := time.Date(2024, 3, 1, 12, 30, 0, 0, time.UTC)
	println(a.Before(b), a.After(b), a.Equal(a), a.Compare(b), b.Compare(a), a.Compare(a))
	d := b.Sub(a)
	println(d.String(), d.Hours(), int(d.Hours()/24))
	println(a.Sub(b).String())
	println(b.Truncate(24*time.Hour).Format(time.DateTime), b.Round(time.Hour).Format(time.DateTime))
	println(a.Add(-time.Nanosecond).Year(), a.Add(366*24*time.Hour).Format(time.DateOnly))
	var zero time.Time
	println(a.After(zero), zero.Before(a))
	max := time.Duration(1<<63 - 1)
	println(a.Add(max).After(a), b.Sub(time.Time{}) == max)
}

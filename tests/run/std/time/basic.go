package main

import "time"

func main() {
	start := time.Now()
	time.Sleep(20 * time.Millisecond)
	el := time.Since(start)
	println(el >= 20*time.Millisecond, el < 2*time.Second)
	d := 90*time.Minute + 30*time.Second
	println(d.Hours(), d.Minutes(), d.String())
	println(time.Duration(1500)*time.Millisecond == 1500*time.Millisecond, time.Second.Milliseconds())
	t := time.Date(2024, time.February, 29, 13, 4, 5, 0, time.UTC)
	println(t.Year(), int(t.Month()), t.Day(), t.Weekday().String(), t.YearDay(), t.Unix())
	println(t.Format("2006-01-02 15:04:05"), t.Add(24*time.Hour).Format(time.RFC3339))
	p, err := time.Parse("2006-01-02", "2023-12-25")
	println(p.Weekday().String(), err == nil, t.Sub(p).Hours())
	println(t.After(p), t.Before(p), t.Equal(t))
}

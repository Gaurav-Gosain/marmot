package main

import "time"

func main() {
	t := time.Date(2023, time.December, 31, 23, 59, 59, 999, time.UTC)
	println(t.Year(), int(t.Month()), t.Month().String(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond())
	println(t.Weekday().String(), t.YearDay(), t.Unix(), t.UnixMilli(), t.UnixNano())
	y, w := t.ISOWeek()
	println(y, w)
	n := t.Add(time.Second)
	println(n.Year(), int(n.Month()), n.Day(), n.YearDay())
	l := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	println(l.AddDate(1, 0, 0).Format("2006-01-02"), l.AddDate(0, 1, 0).Format("2006-01-02"), l.AddDate(0, 0, -60).Format("2006-01-02"))
	o := time.Date(2023, 14, 35, 25, 61, 61, 0, time.UTC)
	println(o.Format(time.RFC3339))
	z := time.Time{}
	println(z.IsZero(), z.Year(), z.Unix())
	println(time.Unix(0, 0).UTC().Format(time.RFC1123), time.Unix(1e9, 5).UTC().Nanosecond())
}

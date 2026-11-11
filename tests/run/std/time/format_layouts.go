package main

import "time"

func main() {
	t := time.Date(2009, time.November, 10, 15, 4, 5, 123456789, time.UTC)
	for _, l := range []string{time.ANSIC, time.RFC822, time.RFC850, time.RFC1123, time.RFC3339, time.RFC3339Nano, time.Kitchen, time.Stamp, time.DateTime, time.DateOnly, time.TimeOnly,
		"Mon Jan _2 3:04PM", "2006/01/02 15h04", "06-1-2", ".000", ".999999", "Monday January 2", "pm PM", "MST -0700 Z07:00"} {
		println(t.Format(l))
	}
}

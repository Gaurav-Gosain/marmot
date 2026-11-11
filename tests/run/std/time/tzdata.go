package main

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

func main() {
	for _, name := range []string{"America/New_York", "Europe/London", "Asia/Kolkata", "Asia/Tokyo", "Australia/Sydney", "UTC", "Nope/Zone", "Pacific/Chatham"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			fmt.Println(name, "error:", err != nil)
			continue
		}
		for _, d := range []time.Time{time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), time.Date(2026, 3, 8, 7, 30, 0, 0, time.UTC), time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)} {
			t := d.In(loc)
			zn, off := t.Zone()
			fmt.Println(name, t.Format("2006-01-02 15:04:05 MST -0700"), zn, off)
		}
	}
	ny, _ := time.LoadLocation("America/New_York")
	gap := time.Date(2026, 3, 8, 2, 30, 0, 0, ny)
	fall := time.Date(2026, 11, 1, 1, 30, 0, 0, ny)
	fmt.Println(gap.Format(time.RFC3339), fall.Format(time.RFC3339), fall.Add(time.Hour).Format(time.RFC3339))
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-07-04 09:00", ny)
	fmt.Println(t, err, t.UTC())
}

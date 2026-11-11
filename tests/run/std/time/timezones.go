package main

import "time"

func main() {
	est := time.FixedZone("EST", -5*3600)
	jst := time.FixedZone("JST", 9*3600)
	t := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	println(t.In(est).Format(time.RFC3339), t.In(jst).Format(time.RFC3339))
	println(t.In(est).Hour(), t.In(jst).Day(), t.In(est).Equal(t))
	name, off := t.In(jst).Zone()
	println(name, off)
	u := time.Date(2024, 6, 15, 12, 0, 0, 0, est)
	println(u.UTC().Format(time.DateTime), u.Unix()-t.Unix())
	println(t.Location().String(), est.String(), t.UTC().Location() == time.UTC)
	p, _ := time.ParseInLocation("2006-01-02 15:04", "2024-06-15 09:00", jst)
	println(p.UTC().Format(time.DateTime))
}

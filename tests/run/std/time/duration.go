package main

import "time"

func main() {
	for _, d := range []time.Duration{0, 1, 999, 1500, time.Second, 90 * time.Second, 36 * time.Hour, -2 * time.Minute, 1<<63 - 1} {
		println(d.String(), d.Seconds(), d.Milliseconds(), d.Microseconds())
	}
	d := 3*time.Hour + 25*time.Minute + 45*time.Second + 500*time.Millisecond
	println(d.Hours(), d.Minutes(), d.Round(time.Hour).String(), d.Truncate(time.Minute).String(), d.Abs() == d)
	p, err := time.ParseDuration("1h15m30.5s")
	println(p.String(), err == nil)
	_, err = time.ParseDuration("abc")
	println(err != nil)
}

package main

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
)

func main() {
	for _, n := range []uint64{0, 1, 999, 1000, 1024, 1500000, 1 << 30, 1<<63 + 5} {
		fmt.Println(humanize.Bytes(n), humanize.IBytes(n))
	}
	for _, n := range []int64{0, 1, -1, 999, 1000, -1234567, 1234567890123} {
		fmt.Println(humanize.Comma(n), humanize.SI(float64(n), "B"))
	}
	fmt.Println(humanize.Commaf(1234567.891), humanize.Ftoa(3.0), humanize.Ftoa(2.50), humanize.FormatFloat("#,###.##", 1234.5678))
	b, err := humanize.ParseBytes("1.5 GiB")
	fmt.Println(b, err)
	b, err = humanize.ParseBytes("12kb")
	fmt.Println(b, err)
	_, err = humanize.ParseBytes("12 zz")
	fmt.Println(err)
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	for _, d := range []time.Duration{0, 5 * time.Second, -90 * time.Second, 3 * time.Hour, -49 * time.Hour, 40 * 24 * time.Hour, 400 * 24 * time.Hour} {
		fmt.Println(humanize.RelTime(now.Add(d), now, "ago", "from now"))
	}
	fmt.Println(humanize.Ordinal(1), humanize.Ordinal(22), humanize.Ordinal(13), humanize.Ordinal(111), humanize.Ordinal(103))
	fmt.Println(humanize.BigBytes(nil) == "" || true)
	v, u := humanize.ComputeSI(0.0000123)
	fmt.Println(v, u)
	f, p, err := humanize.ParseSI("2.5 Mm")
	fmt.Println(f, p, err)
}

package main

// Allocates roughly 4 GB in total in 64 KB pieces. If the collector never frees, a
// machine without that much memory fails. Run under ulimit by the conform runner is not
// possible, so the test also checks that live data stays intact.
type chunk struct {
	data [8192]int
	id   int
}

func main() {
	var live [16]*chunk
	for i := 0; i < 60000; i++ {
		c := new(chunk)
		c.id = i
		c.data[8191] = i
		live[i%16] = c
	}
	ok := true
	for i, c := range live {
		want := 60000 - 16 + i
		if c.id != want || c.data[8191] != want {
			ok = false
		}
	}
	println(ok)
}

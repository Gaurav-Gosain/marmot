// goroutine fan-out: a fixed worker pool pulls jobs and sends results.
package main

const (
	workers = 8
	jobs    = 3000000
)

func work(n int) int {
	s := 0
	for i := 0; i < 300; i++ {
		s += (n ^ i) * 31 % 7
	}
	return s
}

func main() {
	in := make(chan int, 64)
	out := make(chan int, 64)
	done := make(chan bool)
	for w := 0; w < workers; w++ {
		go func() {
			for j := range in {
				out <- work(j)
			}
			done <- true
		}()
	}
	go func() {
		for i := 0; i < jobs; i++ {
			in <- i
		}
		close(in)
	}()
	go func() {
		for w := 0; w < workers; w++ {
			<-done
		}
		close(out)
	}()
	sum := 0
	for r := range out {
		sum += r
	}
	println("workerpool", sum)
}

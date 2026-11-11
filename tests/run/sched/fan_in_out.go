package main

func worker(id int, jobs <-chan int, results chan<- int) {
	for j := range jobs {
		results <- j * 2
	}
}

func main() {
	jobs := make(chan int, 100)
	results := make(chan int, 100)
	for w := 0; w < 5; w++ {
		go worker(w, jobs, results)
	}
	for i := 1; i <= 100; i++ {
		jobs <- i
	}
	close(jobs)
	t := 0
	for i := 0; i < 100; i++ {
		t += <-results
	}
	println(t)
	merged := make(chan int)
	done := make(chan bool)
	for s := 0; s < 4; s++ {
		go func() {
			for i := 0; i < 25; i++ {
				merged <- s*100 + i
			}
			done <- true
		}()
	}
	cnt, sum := 0, 0
	for fin := 0; fin < 4; {
		select {
		case v := <-merged:
			cnt++
			sum += v
		case <-done:
			fin++
		}
	}
	println(cnt, sum)
}

package main

type task struct {
	n   int
	out chan int
}

func main() {
	tasks := make(chan task)
	for w := 0; w < 3; w++ {
		go func() {
			for t := range tasks {
				s := 0
				for i := 1; i <= t.n; i++ {
					s += i
				}
				t.out <- s
			}
		}()
	}
	outs := make([]chan int, 20)
	for i := range outs {
		outs[i] = make(chan int, 1)
		tasks <- task{i * 100, outs[i]}
	}
	t := 0
	for _, o := range outs {
		t += <-o
	}
	close(tasks)
	println(t)
}

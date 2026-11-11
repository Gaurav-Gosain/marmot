package main

type Acc struct {
	sum int
	ch  chan int
	fin chan bool
}

func (a *Acc) run() {
	for v := range a.ch {
		a.sum += v
	}
	a.fin <- true
}

func main() {
	accs := make([]*Acc, 4)
	for i := range accs {
		accs[i] = &Acc{ch: make(chan int), fin: make(chan bool)}
		go accs[i].run()
	}
	for i := 0; i < 1000; i++ {
		accs[i%4].ch <- i
	}
	t := 0
	for _, a := range accs {
		close(a.ch)
		<-a.fin
		t += a.sum
	}
	println(t)
}

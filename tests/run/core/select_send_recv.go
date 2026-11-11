package main

func main() {
	in, out := make(chan int), make(chan int)
	go func() {
		for i := 0; i < 3; i++ {
			in <- i
		}
		close(in)
	}()
	go func() {
		for v := range out {
			println("out", v)
		}
	}()
	pending := []int{}
	open := true
	for open || len(pending) > 0 {
		var send chan int
		var first int
		if len(pending) > 0 {
			send = out
			first = pending[0]
		}
		select {
		case v, ok := <-in:
			if !ok {
				open = false
				in = nil
				continue
			}
			pending = append(pending, v*10)
		case send <- first:
			pending = pending[1:]
		}
	}
	close(out)
	println("end")
}

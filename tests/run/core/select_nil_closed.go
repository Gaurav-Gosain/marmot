package main

func main() {
	var nilc chan int
	closed := make(chan int)
	close(closed)
	select {
	case <-nilc:
		println("nil")
	case v, ok := <-closed:
		println("closed", v, ok)
	}
	select {
	case nilc <- 1:
		println("nil send")
	default:
		println("default")
	}
	cnt := 0
	for i := 0; i < 5; i++ {
		select {
		case <-closed:
			cnt++
		case <-nilc:
		}
	}
	println(cnt)
	select {
	default:
		println("empty select with default")
	}
}

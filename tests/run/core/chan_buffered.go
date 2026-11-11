package main

func main() {
	ch := make(chan string, 3)
	println(len(ch), cap(ch))
	ch <- "a"
	ch <- "b"
	println(len(ch), cap(ch))
	ch <- "c"
	println(<-ch, <-ch, len(ch))
	close(ch)
	v, ok := <-ch
	println(v, ok)
	v, ok = <-ch
	println(v == "", ok)
	var nilch chan int
	println(len(nilch), cap(nilch), nilch == nil)
	unb := make(chan int)
	println(len(unb), cap(unb))
	c2 := make(chan int, 1)
	select {
	case c2 <- 1:
		println("sent")
	default:
		println("full")
	}
	select {
	case c2 <- 2:
		println("sent")
	default:
		println("full")
	}
}

// channel ping-pong between two goroutines over unbuffered channels.
package main

const rounds = 3000000

func main() {
	ping := make(chan int)
	pong := make(chan int)
	go func() {
		for v := range ping {
			pong <- v + 1
		}
		close(pong)
	}()
	v := 0
	for i := 0; i < rounds; i++ {
		ping <- v
		v = <-pong
	}
	close(ping)
	println("pingpong", v)
}

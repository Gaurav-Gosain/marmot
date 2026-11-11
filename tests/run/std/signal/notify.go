package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	c := make(chan os.Signal, 4)
	signal.Notify(c, syscall.SIGUSR1, syscall.SIGUSR2)
	syscall.Kill(os.Getpid(), syscall.SIGUSR1)
	select {
	case s := <-c:
		println("got", s == syscall.SIGUSR1, s.String())
	case <-time.After(5 * time.Second):
		println("timeout")
	}
	syscall.Kill(os.Getpid(), syscall.SIGUSR2)
	select {
	case s := <-c:
		println("got", s == syscall.SIGUSR2)
	case <-time.After(5 * time.Second):
		println("timeout")
	}
	signal.Stop(c)
	signal.Ignore(syscall.SIGUSR1)
	println(signal.Ignored(syscall.SIGUSR1), signal.Ignored(syscall.SIGUSR2))
	syscall.Kill(os.Getpid(), syscall.SIGUSR1)
	time.Sleep(20 * time.Millisecond)
	println("survived ignored signal")
	signal.Reset()
}

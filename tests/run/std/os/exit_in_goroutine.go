package main

import "os"

func main() {
	done := make(chan bool)
	go func() {
		os.Stdout.WriteString("exiting from goroutine\n")
		os.Exit(9)
	}()
	<-done
}

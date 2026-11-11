package main

import "os"

func main() {
	defer println("deferred must not run")
	os.Stdout.WriteString("exiting\n")
	os.Exit(7)
}

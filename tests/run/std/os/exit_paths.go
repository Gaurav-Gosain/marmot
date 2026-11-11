package main

import "os"

func main() {
	defer println("deferred not run on os.Exit")
	os.Stdout.WriteString("before exit\n")
	if len(os.Args) > 5 {
		return
	}
	os.Exit(3)
}

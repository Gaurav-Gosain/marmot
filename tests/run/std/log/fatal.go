package main

import (
	"log"
	"os"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
	log.SetPrefix("fatal: ")
	log.Println("starting")
	defer println("deferred not run")
	log.Fatalf("cannot continue: %v", "boom")
}

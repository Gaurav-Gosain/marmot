package main

func main() { used() }

func used() {}

// Dead is exported but nothing calls it.
func Dead() {}

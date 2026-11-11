package main

func main() {
	lock := make(chan struct{}, 1)
	counter := 0
	done := make(chan bool)
	for g := 0; g < 50; g++ {
		go func() {
			for i := 0; i < 100; i++ {
				lock <- struct{}{}
				counter++
				<-lock
			}
			done <- true
		}()
	}
	for g := 0; g < 50; g++ {
		<-done
	}
	println(counter)
}

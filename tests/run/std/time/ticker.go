package main

import "time"

func main() {
	tk := time.NewTicker(10 * time.Millisecond)
	start := time.Now()
	for i := 0; i < 4; i++ {
		<-tk.C
	}
	el := time.Since(start)
	println(el >= 35*time.Millisecond, el < time.Second)
	tk.Reset(5 * time.Millisecond)
	<-tk.C
	tk.Stop()
	select {
	case <-tk.C:
		println("tick after stop")
	case <-time.After(30 * time.Millisecond):
		println("stopped")
	}
	for range time.Tick(time.Millisecond) {
		break
	}
	println("tick done")
}

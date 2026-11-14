package main

import "time"

func main() {
	// start before the ticker: the fourth tick is due 40 ms after NewTicker,
	// so at least that long after start, however long NewTicker takes.
	start := time.Now()
	tk := time.NewTicker(10 * time.Millisecond)
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

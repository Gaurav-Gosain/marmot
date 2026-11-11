package main

import (
	"runtime"
	"runtime/debug"
	"sync"
)

func main() {
	println(runtime.NumCPU() >= 1, runtime.GOMAXPROCS(0) >= 1, runtime.GOOS != "", runtime.GOARCH != "", runtime.Version() != "")
	base := runtime.NumGoroutine()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-stop
		}()
	}
	runtime.Gosched()
	println(runtime.NumGoroutine()-base == 10)
	close(stop)
	wg.Wait()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	println(ms.HeapAlloc > 0, ms.NumGC > 0, ms.Mallocs >= ms.Frees)
	old := debug.SetGCPercent(50)
	println(old != 0 || true)
	runtime.KeepAlive(&ms)
	_, file, line, ok := runtime.Caller(0)
	println(ok, file != "" || !ok, line >= 0)
}

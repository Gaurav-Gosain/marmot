package runtime

import (
	"sync"
	"testing"
	"time"
	"unsafe"
)

func TestChanRingCapacities(t *testing.T) {
	for _, size := range []int{1, 2, 10} {
		c := makechan(tInt, size)
		var wg sync.WaitGroup
		const n = 2000
		goroutine(&wg, func() {
			for i := 0; i < n; i++ {
				sendInt(c, i)
			}
		})
		got := make(chan int)
		go func() {
			s := 0
			for i := 0; i < n; i++ {
				v, _ := recvInt(c)
				s += v
			}
			got <- s
		}()
		select {
		case s := <-got:
			if s != n*(n-1)/2 {
				t.Errorf("size %d: sum %d", size, s)
			}
		case <-time.After(5 * time.Second):
			seq := unsafe.Slice((*uint64)(c.seq), size)
			t.Fatalf("size %d stalled: enq %x deq %d seq %v recvq.n %d sendq.n %d", size, c.enqpos, c.deqpos, seq, c.recvq.n, c.sendq.n)
		}
		wg.Wait()
	}
}

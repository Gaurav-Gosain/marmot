package runtime

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// TestSemaMutualExclusion uses a semaphore initialized to 1 as a mutex.
func TestSemaMutualExclusion(t *testing.T) {
	sem := uint32(1)
	var inside, maxInside int32
	counter := 0
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		goroutine(&wg, func() {
			for i := 0; i < 500; i++ {
				semacquire1(&sem, i%2 == 0)
				n := atomic.AddInt32(&inside, 1)
				if n > atomic.LoadInt32(&maxInside) {
					atomic.StoreInt32(&maxInside, n)
				}
				counter++
				atomic.AddInt32(&inside, -1)
				semrelease1(&sem)
			}
		})
	}
	wg.Wait()
	if counter != 8*500 || maxInside != 1 || sem != 1 {
		t.Errorf("counter %d maxInside %d sem %d", counter, maxInside, sem)
	}
}

// TestSemaHandsUnitsToWaiters releases units while goroutines wait.
func TestSemaHandsUnitsToWaiters(t *testing.T) {
	sem := uint32(0)
	var got int32
	var wg sync.WaitGroup
	const n = 20
	for i := 0; i < n; i++ {
		goroutine(&wg, func() {
			semacquire1(&sem, false)
			atomic.AddInt32(&got, 1)
		})
	}
	time.Sleep(10 * time.Millisecond)
	for i := 0; i < n; i++ {
		semrelease1(&sem)
	}
	wg.Wait()
	if got != n || sem != 0 {
		t.Errorf("got %d sem %d", got, sem)
	}
	// A release with no waiter is kept for the next acquire.
	semrelease1(&sem)
	semacquire1(&sem, false)
	if sem != 0 {
		t.Errorf("sem %d after release/acquire", sem)
	}
}

func TestNotifyListLayout(t *testing.T) {
	// sync.notifyList: wait, notify uint32, lock word, head, tail.
	if unsafe.Sizeof(notifyList{}) != 32 || unsafe.Offsetof(notifyList{}.head) != 16 {
		t.Errorf("notifyList size %d head at %d", unsafe.Sizeof(notifyList{}), unsafe.Offsetof(notifyList{}.head))
	}
}

// TestNotifyList follows the sync.Cond protocol: Add a ticket under the
// caller's lock, then Wait for it.
func TestNotifyList(t *testing.T) {
	var l notifyList
	var mu sync.Mutex
	ready := 0
	var woke int32
	var wg sync.WaitGroup
	const n = 10
	for i := 0; i < n; i++ {
		goroutine(&wg, func() {
			mu.Lock()
			ready++
			tk := notifyListAdd(&l)
			mu.Unlock()
			notifyListWait(&l, tk)
			atomic.AddInt32(&woke, 1)
		})
	}
	for {
		mu.Lock()
		r := ready
		mu.Unlock()
		if r == n {
			break
		}
		time.Sleep(time.Millisecond)
	}
	notifyListNotifyOne(&l)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&woke) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond)
	if w := atomic.LoadInt32(&woke); w != 1 {
		t.Fatalf("NotifyOne woke %d", w)
	}
	notifyListNotifyAll(&l)
	wg.Wait()
	if woke != n {
		t.Errorf("woke %d", woke)
	}
	// A ticket notified before Wait does not block.
	tk := notifyListAdd(&l)
	notifyListNotifyAll(&l)
	notifyListWait(&l, tk)
}

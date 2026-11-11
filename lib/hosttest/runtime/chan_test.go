package runtime

import (
	"sync"
	"testing"
	"unsafe"
)

func sendInt(c *hchan, v int) { chansend1(c, unsafe.Pointer(&v)) }

func recvInt(c *hchan) (int, bool) {
	var v int
	ok := chanrecv2(c, unsafe.Pointer(&v))
	return v, ok
}

func goroutine(wg *sync.WaitGroup, f func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer forgetg()
		f()
	}()
}

func TestChanBuffered(t *testing.T) {
	c := makechan(tInt, 3)
	sendInt(c, 1)
	sendInt(c, 2)
	if chanlen(c) != 2 || chancap(c) != 3 {
		t.Fatalf("len %d cap %d", chanlen(c), chancap(c))
	}
	if v, ok := recvInt(c); v != 1 || !ok {
		t.Fatalf("got %d %v", v, ok)
	}
	sendInt(c, 3)
	sendInt(c, 4)
	closechan(c)
	for _, want := range []int{2, 3, 4} {
		if v, ok := recvInt(c); v != want || !ok {
			t.Fatalf("got %d %v want %d", v, ok, want)
		}
	}
	if v, ok := recvInt(c); v != 0 || ok {
		t.Fatalf("closed recv got %d %v", v, ok)
	}
}

func TestChanUnbufferedPingPong(t *testing.T) {
	ping := makechan(tInt, 0)
	pong := makechan(tInt, 0)
	var wg sync.WaitGroup
	const n = 2000
	goroutine(&wg, func() {
		for {
			v, ok := recvInt(ping)
			if !ok {
				return
			}
			sendInt(pong, v+1)
		}
	})
	for i := 0; i < n; i++ {
		sendInt(ping, i)
		if v, _ := recvInt(pong); v != i+1 {
			t.Fatalf("got %d want %d", v, i+1)
		}
	}
	closechan(ping)
	wg.Wait()
}

func TestChanManyProducers(t *testing.T) {
	for _, size := range []int{0, 1, 10} {
		c := makechan(tInt, size)
		var wg sync.WaitGroup
		const producers, each = 8, 500
		for p := 0; p < producers; p++ {
			goroutine(&wg, func() {
				for i := 0; i < each; i++ {
					sendInt(c, 1)
				}
			})
		}
		done := makechan(tInt, 0)
		var sum int
		var cwg sync.WaitGroup
		goroutine(&cwg, func() {
			for {
				v, ok := recvInt(c)
				if !ok {
					break
				}
				sum += v
			}
			sendInt(done, sum)
		})
		wg.Wait()
		closechan(c)
		if v, _ := recvInt(done); v != producers*each {
			t.Errorf("size %d: sum %d", size, v)
		}
		cwg.Wait()
	}
}

func TestChanClosePanics(t *testing.T) {
	c := makechan(tInt, 1)
	closechan(c)
	if got := errMsg(catch(func() { closechan(c) })); got != "close of closed channel" {
		t.Errorf("got %q", got)
	}
	if got := errMsg(catch(func() { sendInt(c, 1) })); got != "send on closed channel" {
		t.Errorf("got %q", got)
	}
	if got := errMsg(catch(func() { closechan(nil) })); got != "close of nil channel" {
		t.Errorf("got %q", got)
	}
}

func TestChanCloseWakesBlocked(t *testing.T) {
	c := makechan(tInt, 0)
	var wg sync.WaitGroup
	results := make([]bool, 4)
	for i := range results {
		goroutine(&wg, func() {
			_, ok := recvInt(c)
			results[i] = ok
		})
	}
	// Wait until all receivers are queued.
	for {
		lock(&c.lock)
		n := 0
		for sg := c.recvq.first; sg != nil; sg = sg.next {
			n++
		}
		unlock(&c.lock)
		if n == len(results) {
			break
		}
	}
	closechan(c)
	wg.Wait()
	for i, ok := range results {
		if ok {
			t.Errorf("receiver %d got ok", i)
		}
	}
	// A blocked sender panics when the channel is closed.
	c2 := makechan(tInt, 0)
	var got any
	var wg2 sync.WaitGroup
	goroutine(&wg2, func() { got = catch(func() { sendInt(c2, 1) }) })
	for {
		lock(&c2.lock)
		q := c2.sendq.first
		unlock(&c2.lock)
		if q != nil {
			break
		}
	}
	closechan(c2)
	wg2.Wait()
	if errMsg(got) != "send on closed channel" {
		t.Errorf("blocked sender: %v", got)
	}
}

func TestChanStringElems(t *testing.T) {
	c := makechan(tString, 2)
	s := "hello"
	chansend1(c, unsafe.Pointer(&s))
	var r string
	chanrecv1(c, unsafe.Pointer(&r))
	if r != "hello" {
		t.Errorf("got %q", r)
	}
}

func TestSelectDefault(t *testing.T) {
	c := makechan(tInt, 0)
	var v int
	cases := []scase{{c: c, elem: unsafe.Pointer(&v)}}
	if chosen, _ := selectgo(&cases[0], 1, false); chosen != -1 {
		t.Errorf("chosen %d", chosen)
	}
	if selectnbsend(c, unsafe.Pointer(&v)) {
		t.Errorf("selectnbsend on unbuffered chan without receiver")
	}
	if sel, _ := selectnbrecv(unsafe.Pointer(&v), c); sel {
		t.Errorf("selectnbrecv")
	}
	b := makechan(tInt, 1)
	v = 5
	if !selectnbsend(b, unsafe.Pointer(&v)) {
		t.Errorf("selectnbsend on buffered chan")
	}
	v = 0
	if sel, ok := selectnbrecv(unsafe.Pointer(&v), b); !sel || !ok || v != 5 {
		t.Errorf("selectnbrecv got %v %v %d", sel, ok, v)
	}
	// Only nil channels and a default.
	nilCases := []scase{{c: nil}}
	if chosen, _ := selectgo(&nilCases[0], 1, false); chosen != -1 {
		t.Errorf("nil chan select chosen %d", chosen)
	}
}

func TestSelectFairness(t *testing.T) {
	a := makechan(tInt, 1000)
	b := makechan(tInt, 1000)
	for i := 0; i < 1000; i++ {
		sendInt(a, 1)
		sendInt(b, 2)
	}
	var va, vb int
	counts := [2]int{}
	for i := 0; i < 1000; i++ {
		cases := []scase{{c: a, elem: unsafe.Pointer(&va)}, {c: b, elem: unsafe.Pointer(&vb)}}
		chosen, ok := selectgo(&cases[0], 2, true)
		if !ok {
			t.Fatal("recvOK false")
		}
		counts[chosen]++
	}
	if counts[0] < 400 || counts[1] < 400 {
		t.Errorf("unfair select: %v", counts)
	}
}

func TestSelectBlockingMany(t *testing.T) {
	// Many goroutines select over shared channels in both directions.
	const n = 4
	chans := make([]*hchan, n)
	for i := range chans {
		chans[i] = makechan(tInt, i%2)
	}
	var wg sync.WaitGroup
	const rounds = 300
	sent := make([]int, n)
	got := make([]int, n)
	for w := 0; w < n; w++ {
		goroutine(&wg, func() {
			for r := 0; r < rounds; r++ {
				v := 1
				cases := make([]scase, n)
				for i := range cases {
					cases[i] = scase{c: chans[i], elem: unsafe.Pointer(&v), send: true}
				}
				chosen, _ := selectgo(&cases[0], n, true)
				lock(&chans[chosen].lock)
				sent[chosen]++
				unlock(&chans[chosen].lock)
			}
		})
	}
	total := 0
	for total < n*rounds {
		var vals [n]int
		cases := make([]scase, n)
		for i := range cases {
			cases[i] = scase{c: chans[i], elem: unsafe.Pointer(&vals[i])}
		}
		chosen, ok := selectgo(&cases[0], n, true)
		if !ok || vals[chosen] != 1 {
			t.Fatalf("bad receive %d %v %v", chosen, ok, vals)
		}
		got[chosen]++
		total++
	}
	wg.Wait()
	for i := range sent {
		if sent[i] != got[i] {
			t.Errorf("chan %d: sent %d got %d", i, sent[i], got[i])
		}
	}
}

func TestSelectWokenByClose(t *testing.T) {
	a := makechan(tInt, 0)
	b := makechan(tInt, 0)
	var chosen int
	var ok bool
	var wg sync.WaitGroup
	goroutine(&wg, func() {
		var va, vb int = 7, 7
		cases := []scase{{c: a, elem: unsafe.Pointer(&va)}, {c: b, elem: unsafe.Pointer(&vb)}}
		chosen, ok = selectgo(&cases[0], 2, true)
		if chosen == 1 && vb != 0 {
			t.Errorf("closed receive did not zero the destination")
		}
	})
	for {
		lock(&b.lock)
		q := b.recvq.first
		unlock(&b.lock)
		if q != nil {
			break
		}
	}
	closechan(b)
	wg.Wait()
	if chosen != 1 || ok {
		t.Errorf("chosen %d ok %v", chosen, ok)
	}
	// The sudog on a must be gone.
	if a.recvq.first != nil {
		t.Errorf("stale sudog left on other channel")
	}
}

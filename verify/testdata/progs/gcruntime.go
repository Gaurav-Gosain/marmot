// gcruntime: objects the runtime allocates or holds (goroutine start records and parameters,
// blocked senders, select cases, buffered channel rings after wrap-around, map groups after
// growth, timers, finalizers, heap defer records, itab caches, closures, boxed values,
// reflect-built channels and select cases), each holding the only reference to a fresh object
// across forced collections. A word the collector does not see lets the object be freed; run
// with MARMOT_GCSTRESS=1 MARMOT_POISON=1 to make that visible. The output must equal stock Go's.
package main

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type val struct{ n int }

//go:noinline
func mk(n int) *val { return &val{n} }

var churn []*val

func gc() {
	for i := 0; i < 3; i++ {
		runtime.GC()
		churn = make([]*val, 512)
		for j := range churn {
			churn[j] = &val{-1}
		}
	}
	churn = nil
}

type pair struct {
	a bool
	p *val
	s string
	q *val
}

type big struct { // over 128 bytes: map slots hold it indirectly
	pad [20]int
	p   *val
}

type namer interface{ name() int }

type n1 struct{ p *val }
type n2 struct {
	x int
	p *val
}
type n3 [2]*val
type n4 struct {
	f func() int
}

func (v n1) name() int  { return v.p.n }
func (v n2) name() int  { return v.p.n + v.x }
func (v n3) name() int  { return v[0].n + v[1].n }
func (v *n4) name() int { return v.f() }

var out []int

func put(xs ...int) { out = append(out, xs...) }

//go:noinline
func goroutineParams() {
	// parameters of goroutines that may not have started when the collection runs
	var wg sync.WaitGroup
	res := make([]int, 40)
	start := make(chan struct{})
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int, p *val, pr pair, x interface{}) {
			defer wg.Done()
			<-start
			res[i] = p.n + pr.p.n + pr.q.n + x.(*val).n + len(pr.s)
		}(i, mk(i), pair{p: mk(1), q: mk(2), s: strings.Repeat("s", i%3)}, mk(100))
	}
	gc()
	close(start)
	wg.Wait()
	put(res...)
}

//go:noinline
func blockedSenders() {
	// unbuffered senders parked with their values; a select parked on several cases
	c := make(chan pair)
	d := make(chan *val)
	e := make(chan [3]*val)
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		go func(i int) { c <- pair{p: mk(10 + i), q: mk(20 + i), s: fmt.Sprint(i)} }(i)
	}
	go func() {
		select {
		case d <- mk(31):
		case e <- [3]*val{mk(32), mk(33), mk(34)}:
		}
		done <- true
	}()
	time.Sleep(20 * time.Millisecond)
	gc()
	var got []int
	for i := 0; i < 4; i++ {
		v := <-c
		got = append(got, v.p.n+v.q.n+len(v.s))
	}
	sort.Ints(got)
	put(got...)
	select {
	case v := <-d: // either case is ready: both print 0 when the value survived
		put(v.n - 31)
	case a := <-e:
		put(a[0].n + a[1].n + a[2].n - 99)
	}
	<-done
	// a receiver parked in select: the value is copied into its slot by the sender
	r := make(chan *val)
	got2 := make(chan int)
	go func() {
		var v *val
		select {
		case v = <-r:
		case <-time.After(time.Hour):
		}
		gc()
		got2 <- v.n
	}()
	time.Sleep(10 * time.Millisecond)
	r <- mk(35)
	put(<-got2)
}

//go:noinline
func chanRing() {
	c := make(chan pair, 8)
	n := 0
	for i := 0; i < 6; i++ {
		c <- pair{p: mk(n), q: mk(n + 1)}
		n += 2
	}
	for i := 0; i < 4; i++ {
		<-c
	}
	for i := 0; i < 6; i++ { // wraps around the ring
		c <- pair{p: mk(n), q: mk(n + 1)}
		n += 2
	}
	gc()
	s := 0
	for len(c) > 0 {
		v := <-c
		s += v.p.n*3 + v.q.n
	}
	put(s)
	ci := make(chan interface{}, 3)
	ci <- mk(7)
	ci <- pair{p: mk(8)}
	ci <- "nine"
	gc()
	put((<-ci).(*val).n, (<-ci).(pair).p.n, len((<-ci).(string)))
}

//go:noinline
func mapGrowth() {
	m := map[int]*val{}
	ms := map[string]pair{}
	mb := map[int]big{}
	mk2 := map[*val]*val{}
	mpk := map[pair]int{}
	keys := []*val{}
	for i := 0; i < 3000; i++ {
		m[i] = mk(i)
		ms[fmt.Sprint(i)] = pair{p: mk(i), q: mk(-i)}
		if i%10 == 0 {
			mb[i] = big{p: mk(i)}
			k := mk(i)
			keys = append(keys, k)
			mk2[k] = mk(i * 2)
			mpk[pair{p: mk(i), s: fmt.Sprint(i)}] = i
		}
		if i == 1500 {
			gc() // in the middle of growth
		}
	}
	keys = nil
	gc()
	s := 0
	for k, v := range m {
		s += v.n - k
	}
	for k, v := range ms {
		s += v.p.n + v.q.n + len(k) - len(fmt.Sprint(v.p.n))
	}
	t := 0
	for k, v := range mb {
		t += v.p.n - k
	}
	u := 0
	for k, v := range mk2 {
		u += v.n - 2*k.n
	}
	w := 0
	for k, v := range mpk {
		w += k.p.n - v
	}
	put(s, t, u, w, len(m), len(ms), len(mb), len(mk2), len(mpk))
	// deletes and refill after growth
	for i := 0; i < 3000; i += 2 {
		delete(m, i)
	}
	for i := 3000; i < 4000; i++ {
		m[i] = mk(i)
	}
	gc()
	s = 0
	for k, v := range m {
		s += v.n - k
	}
	put(s, len(m))
}

//go:noinline
func timers() {
	res := make(chan int, 4)
	func(p *val) { time.AfterFunc(30*time.Millisecond, func() { res <- p.n }) }(mk(41))
	q := mk(42)
	t := time.NewTimer(time.Hour)
	t.Stop()
	t.Reset(20 * time.Millisecond)
	go func(q *val) {
		<-t.C
		res <- q.n
	}(q)
	q = nil
	tk := time.NewTicker(5 * time.Millisecond)
	go func(r *val) {
		for i := 0; i < 3; i++ {
			<-tk.C
		}
		tk.Stop()
		res <- r.n
	}(mk(43))
	gc()
	var got []int
	for i := 0; i < 3; i++ {
		got = append(got, <-res)
	}
	sort.Ints(got)
	put(got...)
}

type fin struct {
	id  int
	ref *val
}

//go:noinline
func finalizers() {
	res := make(chan int, 10)
	for i := 0; i < 5; i++ {
		f := &fin{id: i, ref: mk(50 + i)}
		keep := mk(60 + i) // reachable only from the finalizer closure
		runtime.SetFinalizer(f, func(f *fin) { res <- f.ref.n*100 + keep.n })
	}
	var got []int
	for tries := 0; len(got) < 5 && tries < 200; tries++ {
		gc()
		time.Sleep(time.Millisecond)
		for more := true; more; {
			select {
			case v := <-res:
				got = append(got, v)
			default:
				more = false
			}
		}
	}
	sort.Ints(got)
	put(got...)
}

//go:noinline
func deferRecords() (r int) {
	for i := 0; i < 4; i++ { // defers in a loop: heap records
		p, q := mk(70+i), pair{p: mk(80 + i), s: "x"}
		defer func(v *val, pr pair) { r += v.n + pr.p.n + len(pr.s) }(p, q)
	}
	gc()
	return 0
}

//go:noinline
func itabs() {
	var ns []namer
	for i := 0; i < 5; i++ {
		ns = append(ns, n1{mk(i)}, n2{i, mk(i)}, n3{mk(i), mk(i + 1)}, &n4{func() int { return i * 7 }})
	}
	var es []interface{}
	for _, n := range ns {
		es = append(es, n)
	}
	ns = nil
	gc()
	s := 0
	for _, e := range es {
		s += e.(namer).name() // assertion to a non-empty interface: itab lookup
		if st, ok := e.(fmt.Stringer); ok {
			s += len(st.String())
		}
	}
	put(s)
}

//go:noinline
func closures() []func() int {
	var fs []func() int
	for i := 0; i < 4; i++ {
		p, q := mk(i), pair{q: mk(i * 10)}
		arr := [2]*val{mk(i + 100), mk(i + 200)}
		fs = append(fs, func() int { return p.n + q.q.n + arr[1].n })
	}
	return fs
}

//go:noinline
func boxes() []interface{} {
	return []interface{}{
		big{p: mk(90)},
		[3]*val{nil, nil, mk(91)},
		pair{q: mk(92)},
		struct {
			x float64
			f func() int
		}{1, func() int { return 93 }},
		[]*val{mk(94)},
		map[int]*val{1: mk(95)},
	}
}

//go:noinline
func reflectRuntime() {
	pt := reflect.TypeOf((*val)(nil))
	ct := reflect.ChanOf(reflect.BothDir, reflect.TypeOf(pair{}))
	ch := reflect.MakeChan(ct, 4)
	ch.Send(reflect.ValueOf(pair{p: mk(110), q: mk(111)}))
	ch2 := reflect.MakeChan(reflect.ChanOf(reflect.BothDir, pt), 0)
	go func() { ch2.Send(reflect.ValueOf(mk(112))) }()
	time.Sleep(10 * time.Millisecond)
	gc()
	cases := []reflect.SelectCase{
		{Dir: reflect.SelectRecv, Chan: ch2},
	}
	_, v, _ := reflect.Select(cases)
	put(v.Interface().(*val).n)
	pv, _ := ch.Recv()
	put(pv.Interface().(pair).p.n + pv.Interface().(pair).q.n)
	s := reflect.MakeSlice(reflect.SliceOf(pt), 0, 1)
	for i := 0; i < 50; i++ {
		s = reflect.Append(s, reflect.ValueOf(mk(i)))
	}
	m := reflect.MakeMapWithSize(reflect.MapOf(reflect.TypeOf(""), pt), 1)
	for i := 0; i < 100; i++ {
		m.SetMapIndex(reflect.ValueOf(fmt.Sprint(i)), reflect.ValueOf(mk(i)))
	}
	gc()
	t := 0
	for i := 0; i < s.Len(); i++ {
		t += s.Index(i).Interface().(*val).n
	}
	u := 0
	it := m.MapRange()
	for it.Next() {
		u += it.Value().Interface().(*val).n
	}
	put(t, u)
}

//go:noinline
func bigObjects() {
	s := make([]*val, 1<<17) // 1 MB of pointers, large object
	s[len(s)-1] = mk(120)
	is := make([]interface{}, 1<<15)
	is[len(is)-1] = pair{p: mk(121)}
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("ab")
	}
	str := sb.String()
	bs := []byte(str)
	rs := []rune(str[:10])
	gc()
	put(s[len(s)-1].n, is[len(is)-1].(pair).p.n, len(str), int(bs[1999]), len(string(rs)))
}

func main() {
	goroutineParams()
	blockedSenders()
	chanRing()
	mapGrowth()
	timers()
	finalizers()
	put(deferRecords())
	itabs()
	fs := closures()
	bx := boxes()
	gc()
	for _, f := range fs {
		put(f())
	}
	put(bx[0].(big).p.n, bx[1].([3]*val)[2].n, bx[2].(pair).q.n, bx[3].(struct {
		x float64
		f func() int
	}).f(), bx[4].([]*val)[0].n, bx[5].(map[int]*val)[1].n)
	reflectRuntime()
	bigObjects()
	fmt.Println(out)
}

// A generic LRU cache with capacity eviction, statistics, and a simulated workload.
package main

import (
	"fmt"
	"sort"
	"strings"
)

type entry[K comparable, V any] struct {
	key        K
	val        V
	prev, next *entry[K, V]
}

type Cache[K comparable, V any] struct {
	cap        int
	items      map[K]*entry[K, V]
	head, tail *entry[K, V]
	hits       int
	misses     int
	evictions  int
	onEvict    func(K, V)
}

func New[K comparable, V any](cap int) *Cache[K, V] {
	if cap <= 0 {
		panic("capacity must be positive")
	}
	return &Cache[K, V]{cap: cap, items: make(map[K]*entry[K, V], cap)}
}

func (c *Cache[K, V]) unlink(e *entry[K, V]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

func (c *Cache[K, V]) pushFront(e *entry[K, V]) {
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *Cache[K, V]) Get(k K) (V, bool) {
	if e, ok := c.items[k]; ok {
		c.hits++
		c.unlink(e)
		c.pushFront(e)
		return e.val, true
	}
	c.misses++
	var zero V
	return zero, false
}

func (c *Cache[K, V]) Peek(k K) (V, bool) {
	if e, ok := c.items[k]; ok {
		return e.val, true
	}
	var zero V
	return zero, false
}

func (c *Cache[K, V]) Put(k K, v V) {
	if e, ok := c.items[k]; ok {
		e.val = v
		c.unlink(e)
		c.pushFront(e)
		return
	}
	if len(c.items) >= c.cap {
		c.evictOldest()
	}
	e := &entry[K, V]{key: k, val: v}
	c.items[k] = e
	c.pushFront(e)
}

func (c *Cache[K, V]) evictOldest() {
	e := c.tail
	if e == nil {
		return
	}
	c.unlink(e)
	delete(c.items, e.key)
	c.evictions++
	if c.onEvict != nil {
		c.onEvict(e.key, e.val)
	}
}

func (c *Cache[K, V]) Delete(k K) bool {
	e, ok := c.items[k]
	if !ok {
		return false
	}
	c.unlink(e)
	delete(c.items, k)
	return true
}

func (c *Cache[K, V]) Len() int { return len(c.items) }

func (c *Cache[K, V]) Keys() []K {
	var ks []K
	for e := c.head; e != nil; e = e.next {
		ks = append(ks, e.key)
	}
	return ks
}

func (c *Cache[K, V]) Resize(n int) {
	c.cap = n
	for len(c.items) > c.cap {
		c.evictOldest()
	}
}

func (c *Cache[K, V]) HitRate() float64 {
	t := c.hits + c.misses
	if t == 0 {
		return 0
	}
	return float64(c.hits) / float64(t)
}

func (c *Cache[K, V]) check() error {
	n := 0
	var prev *entry[K, V]
	for e := c.head; e != nil; e = e.next {
		if e.prev != prev {
			return fmt.Errorf("broken prev link at %v", e.key)
		}
		if c.items[e.key] != e {
			return fmt.Errorf("map mismatch at %v", e.key)
		}
		prev = e
		n++
	}
	if prev != c.tail {
		return fmt.Errorf("tail mismatch")
	}
	if n != len(c.items) {
		return fmt.Errorf("size mismatch %d != %d", n, len(c.items))
	}
	return nil
}

type rng uint64

func (r *rng) next() uint64 {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return uint64(*r)
}

func skewed(r *rng, n int) int {
	u := float64(r.next()%10000) / 10000
	return int(u * u * float64(n))
}

func main() {
	c := New[string, int](3)
	var evicted []string
	c.onEvict = func(k string, v int) { evicted = append(evicted, fmt.Sprintf("%s=%d", k, v)) }
	for i, k := range []string{"a", "b", "c", "a", "d", "e", "a", "f"} {
		c.Put(k, i)
		fmt.Println("put", k, "->", c.Keys())
	}
	fmt.Println("evicted:", evicted)
	v, ok := c.Get("a")
	fmt.Println(v, ok, c.Keys())
	_, ok = c.Get("zzz")
	fmt.Println(ok, c.hits, c.misses)
	pv, _ := c.Peek("f")
	fmt.Println("peek", pv, c.Keys())
	fmt.Println(c.Delete("a"), c.Delete("a"), c.Len(), c.Keys())
	c.Resize(1)
	fmt.Println(c.Keys(), c.evictions)
	fmt.Println(c.check())

	type Point struct{ X, Y int }
	pc := New[Point, []string](2)
	pc.Put(Point{1, 2}, []string{"origin-ish"})
	pc.Put(Point{3, 4}, nil)
	pc.Put(Point{5, 6}, []string{"x", "y"})
	_, ok = pc.Get(Point{1, 2})
	l, _ := pc.Get(Point{5, 6})
	fmt.Println(ok, l, pc.Keys())

	for _, size := range []int{10, 50, 200, 1000} {
		cache := New[int, string](size)
		r := rng(88172645463325252)
		for i := 0; i < 50000; i++ {
			k := skewed(&r, 2000)
			if _, ok := cache.Get(k); !ok {
				cache.Put(k, strings.Repeat("v", k%5+1))
			}
		}
		fmt.Printf("size=%-5d hit=%.4f evictions=%-6d len=%-5d check=%v\n", size, cache.HitRate(), cache.evictions, cache.Len(), cache.check())
	}

	counts := map[int]int{}
	r := rng(12345)
	for i := 0; i < 20000; i++ {
		counts[skewed(&r, 10)]++
	}
	var ks []int
	for k := range counts {
		ks = append(ks, k)
	}
	sort.Ints(ks)
	for _, k := range ks {
		fmt.Printf("%d:%d ", k, counts[k])
	}
	fmt.Println()

	defer func() { fmt.Println("recovered:", recover()) }()
	New[int, int](0)
}

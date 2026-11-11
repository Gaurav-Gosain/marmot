package main

import (
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/hashicorp/golang-lru/v2/simplelru"
)

func main() {
	var evicted []string
	c, _ := lru.NewWithEvict[string, int](3, func(k string, v int) { evicted = append(evicted, fmt.Sprint(k, v)) })
	for i, k := range []string{"a", "b", "c", "a", "d", "e"} {
		c.Add(k, i)
	}
	fmt.Println(c.Keys(), c.Len(), evicted)
	v, ok := c.Get("d")
	fmt.Println(v, ok, c.Keys())
	_, ok = c.Peek("zz")
	fmt.Println(ok, c.Contains("e"), c.Remove("e"), c.Remove("e"))
	k, vv, ok := c.GetOldest()
	fmt.Println(k, vv, ok, c.Values())
	prev, ok := c.ContainsOrAdd("x", 99)
	fmt.Println(prev, ok, c.Keys())
	c.Resize(1)
	fmt.Println(c.Keys(), evicted)
	c.Purge()
	fmt.Println(c.Len())
	_, err := lru.New[int, int](0)
	fmt.Println(err)
	s, _ := simplelru.NewLRU[int, string](2, nil)
	s.Add(1, "x")
	s.Add(2, "y")
	s.Add(3, "z")
	ok1k, ok1v, ok1 := s.RemoveOldest()
	fmt.Println(s.Keys(), ok1k, ok1v, ok1)
	e := expirable.NewLRU[string, int](2, nil, 0)
	e.Add("k", 1)
	e.Add("l", 2)
	e.Add("m", 3)
	fmt.Println(e.Keys(), e.Len())
	big, _ := lru.New[int, int](1000)
	for i := 0; i < 5000; i++ {
		big.Add(i%1500, i)
	}
	fmt.Println(big.Len(), big.Contains(1499), big.Contains(100))
}

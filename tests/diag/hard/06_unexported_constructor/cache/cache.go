package cache

type Cache struct {
	max  int
	data map[string]int
}

func newCache(max int) *Cache { return &Cache{max: max, data: map[string]int{}} }

// New returns a cache that holds at most max entries.
func New(max int) *Cache { return newCache(max) }

func (c *Cache) Put(k string, v int) {
	if len(c.data) < c.max {
		c.data[k] = v
	}
}

func (c *Cache) Len() int { return len(c.data) }

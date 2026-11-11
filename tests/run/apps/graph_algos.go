// Graph algorithms: Dijkstra with a heap, BFS/DFS, topological sort, strongly connected
// components (Tarjan), union-find with Kruskal MST, and bipartite matching.
package main

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"
)

type Edge struct{ To, W int }

type Graph struct {
	N   int
	Adj [][]Edge
}

func NewGraph(n int) *Graph { return &Graph{n, make([][]Edge, n)} }

func (g *Graph) Add(u, v, w int) { g.Adj[u] = append(g.Adj[u], Edge{v, w}) }
func (g *Graph) AddBoth(u, v, w int) {
	g.Add(u, v, w)
	g.Add(v, u, w)
}

type item struct{ node, dist int }
type minHeap []item

func (h minHeap) Len() int { return len(h) }
func (h minHeap) Less(i, j int) bool {
	return h[i].dist < h[j].dist || h[i].dist == h[j].dist && h[i].node < h[j].node
}
func (h minHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)   { *h = append(*h, x.(item)) }
func (h *minHeap) Pop() any     { o := *h; n := len(o); x := o[n-1]; *h = o[:n-1]; return x }

const Inf = 1 << 60

func (g *Graph) Dijkstra(src int) ([]int, []int) {
	dist := make([]int, g.N)
	prev := make([]int, g.N)
	for i := range dist {
		dist[i], prev[i] = Inf, -1
	}
	dist[src] = 0
	h := &minHeap{{src, 0}}
	for h.Len() > 0 {
		it := heap.Pop(h).(item)
		if it.dist > dist[it.node] {
			continue
		}
		for _, e := range g.Adj[it.node] {
			if nd := it.dist + e.W; nd < dist[e.To] {
				dist[e.To], prev[e.To] = nd, it.node
				heap.Push(h, item{e.To, nd})
			}
		}
	}
	return dist, prev
}

func path(prev []int, to int) []int {
	var p []int
	for v := to; v != -1; v = prev[v] {
		p = append([]int{v}, p...)
	}
	return p
}

func (g *Graph) BFS(src int) []int {
	level := make([]int, g.N)
	for i := range level {
		level[i] = -1
	}
	level[src] = 0
	q := []int{src}
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		for _, e := range g.Adj[u] {
			if level[e.To] < 0 {
				level[e.To] = level[u] + 1
				q = append(q, e.To)
			}
		}
	}
	return level
}

func (g *Graph) TopoSort() ([]int, bool) {
	indeg := make([]int, g.N)
	for u := range g.Adj {
		for _, e := range g.Adj[u] {
			indeg[e.To]++
		}
	}
	var ready, order []int
	for i, d := range indeg {
		if d == 0 {
			ready = append(ready, i)
		}
	}
	for len(ready) > 0 {
		sort.Ints(ready)
		u := ready[0]
		ready = ready[1:]
		order = append(order, u)
		for _, e := range g.Adj[u] {
			if indeg[e.To]--; indeg[e.To] == 0 {
				ready = append(ready, e.To)
			}
		}
	}
	return order, len(order) == g.N
}

func (g *Graph) SCC() [][]int {
	index := make([]int, g.N)
	low := make([]int, g.N)
	on := make([]bool, g.N)
	for i := range index {
		index[i] = -1
	}
	var stack []int
	var comps [][]int
	counter := 0
	var strong func(v int)
	strong = func(v int) {
		index[v], low[v] = counter, counter
		counter++
		stack = append(stack, v)
		on[v] = true
		for _, e := range g.Adj[v] {
			if index[e.To] < 0 {
				strong(e.To)
				low[v] = min(low[v], low[e.To])
			} else if on[e.To] {
				low[v] = min(low[v], index[e.To])
			}
		}
		if low[v] == index[v] {
			var comp []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			sort.Ints(comp)
			comps = append(comps, comp)
		}
	}
	for v := 0; v < g.N; v++ {
		if index[v] < 0 {
			strong(v)
		}
	}
	sort.Slice(comps, func(i, j int) bool { return comps[i][0] < comps[j][0] })
	return comps
}

type DSU struct{ parent, size []int }

func NewDSU(n int) *DSU {
	d := &DSU{make([]int, n), make([]int, n)}
	for i := range d.parent {
		d.parent[i], d.size[i] = i, 1
	}
	return d
}

func (d *DSU) Find(x int) int {
	for d.parent[x] != x {
		d.parent[x] = d.parent[d.parent[x]]
		x = d.parent[x]
	}
	return x
}

func (d *DSU) Union(a, b int) bool {
	a, b = d.Find(a), d.Find(b)
	if a == b {
		return false
	}
	if d.size[a] < d.size[b] {
		a, b = b, a
	}
	d.parent[b] = a
	d.size[a] += d.size[b]
	return true
}

type WEdge struct{ U, V, W int }

func Kruskal(n int, edges []WEdge) (int, []WEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].W != edges[j].W {
			return edges[i].W < edges[j].W
		}
		return edges[i].U < edges[j].U || edges[i].U == edges[j].U && edges[i].V < edges[j].V
	})
	d := NewDSU(n)
	total := 0
	var mst []WEdge
	for _, e := range edges {
		if d.Union(e.U, e.V) {
			total += e.W
			mst = append(mst, e)
		}
	}
	return total, mst
}

func Matching(left, right int, adj [][]int) int {
	matchR := make([]int, right)
	for i := range matchR {
		matchR[i] = -1
	}
	var try func(u int, seen []bool) bool
	try = func(u int, seen []bool) bool {
		for _, v := range adj[u] {
			if seen[v] {
				continue
			}
			seen[v] = true
			if matchR[v] < 0 || try(matchR[v], seen) {
				matchR[v] = u
				return true
			}
		}
		return false
	}
	n := 0
	for u := 0; u < left; u++ {
		if try(u, make([]bool, right)) {
			n++
		}
	}
	return n
}

func main() {
	names := strings.Fields("A B C D E F G H")
	g := NewGraph(8)
	for _, e := range [][3]int{{0, 1, 4}, {0, 2, 1}, {2, 1, 2}, {1, 3, 5}, {2, 3, 8}, {3, 4, 3}, {4, 5, 1}, {2, 5, 20}, {5, 6, 2}} {
		g.Add(e[0], e[1], e[2])
	}
	dist, prev := g.Dijkstra(0)
	for v := 0; v < g.N; v++ {
		if dist[v] == Inf {
			fmt.Printf("%s: unreachable\n", names[v])
			continue
		}
		var ps []string
		for _, p := range path(prev, v) {
			ps = append(ps, names[p])
		}
		fmt.Printf("%s: %2d via %s\n", names[v], dist[v], strings.Join(ps, "->"))
	}
	fmt.Println("bfs levels:", g.BFS(0))
	order, ok := g.TopoSort()
	fmt.Println("topo:", order, ok)
	g.Add(6, 2, 1)
	order, ok = g.TopoSort()
	fmt.Println("topo with cycle:", order, ok)
	fmt.Println("scc:", g.SCC())

	var edges []WEdge
	for u := 0; u < 8; u++ {
		for v := u + 1; v < 8; v++ {
			if (u*v+u+v)%3 != 0 {
				edges = append(edges, WEdge{u, v, (u*7+v*13)%17 + 1})
			}
		}
	}
	total, mst := Kruskal(8, edges)
	fmt.Println("mst weight:", total, "edges:", len(mst))
	for _, e := range mst {
		fmt.Printf("  %s-%s %d\n", names[e.U], names[e.V], e.W)
	}
	adj := [][]int{{0, 1}, {0}, {1, 2}, {2}, {2, 3}}
	fmt.Println("matching:", Matching(5, 4, adj))

	const n = 3000
	big := NewGraph(n)
	for i := 0; i < n; i++ {
		big.AddBoth(i, (i+1)%n, 1+i%5)
		big.AddBoth(i, (i*7+3)%n, 3+i%11)
	}
	d, _ := big.Dijkstra(0)
	sum, far := 0, 0
	for _, x := range d {
		sum += x
		far = max(far, x)
	}
	lv := big.BFS(0)
	maxLv := 0
	for _, l := range lv {
		maxLv = max(maxLv, l)
	}
	fmt.Println("big:", sum, far, maxLv, len(big.SCC()))
	dsu := NewDSU(n)
	comps := n
	for i := 0; i < n; i += 2 {
		if dsu.Union(i, (i+2)%n) {
			comps--
		}
	}
	fmt.Println("dsu components:", comps)
}

package main

import "sort"

type byLen []string

func (b byLen) Len() int           { return len(b) }
func (b byLen) Less(i, j int) bool { return len(b[i]) < len(b[j]) }
func (b byLen) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func main() {
	w := []string{"ccc", "a", "bb", "dddd", ""}
	sort.Sort(byLen(w))
	println(w[0] == "", w[1], w[4])
	sort.Sort(sort.Reverse(byLen(w)))
	println(w[0], w[4] == "")
	sort.Stable(byLen(w))
	println(sort.IsSorted(byLen(w)))
	x := sort.IntSlice{3, 1, 2}
	sort.Sort(x)
	println(x[0], x[2], x.Len(), x.Search(2))
	sort.Sort(sort.Reverse(sort.StringSlice(w)))
	println(w[0])
}

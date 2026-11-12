package main

type List struct{ items []string }

func (l *List) Add(s string)  { l.items = append(l.items, s) }
func (l *List) Length() int    { return len(l.items) }
func (l *List) Contains(s string) bool {
	for _, x := range l.items {
		if x == s {
			return true
		}
	}
	return false
}

package main

type Item struct {
	Name  string
	Price float64
	Qty   int
}

func Total(items []Item) float64 {
	t := 0.0
	for _, it := range items {
		t += it.Price * float64(it.Qty)
	}
	return t
}

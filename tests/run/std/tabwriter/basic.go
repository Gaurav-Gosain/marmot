package main

import (
	"fmt"
	"os"
	"text/tabwriter"
)

func main() {
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "name\tqty\tprice")
	fmt.Fprintln(w, "apple\t3\t0.5")
	fmt.Fprintln(w, "watermelon\t12\t12.75")
	fmt.Fprintln(w, "fig\t1\t3")
	w.Flush()
	w = tabwriter.NewWriter(os.Stdout, 5, 0, 1, '.', tabwriter.AlignRight|tabwriter.Debug)
	fmt.Fprintln(w, "a\tb\tc\t")
	fmt.Fprintln(w, "aaa\tbbbb\tc\t")
	fmt.Fprintln(w, "世界\té\tx\t")
	w.Flush()
}

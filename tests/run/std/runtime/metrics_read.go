// runtime/metrics.Read: the metrics marmot's runtime serves have the kind
// gc gives them (all uint64), an unknown name is KindBad, and the heap
// numbers show what is live after an allocation.
package main

import (
	"fmt"
	"runtime/metrics"
)

var sink [][]byte

func main() {
	names := []string{
		"/memory/classes/heap/objects:bytes",
		"/memory/classes/heap/unused:bytes",
		"/memory/classes/heap/stacks:bytes",
		"/memory/classes/heap/free:bytes",
		"/memory/classes/heap/released:bytes",
		"/memory/classes/total:bytes",
		"/gc/heap/live:bytes",
		"/gc/heap/allocs:bytes",
		"/gc/heap/allocs:objects",
		"/gc/heap/frees:objects",
		"/gc/heap/objects:objects",
		"/gc/cycles/total:gc-cycles",
		"/gc/cycles/automatic:gc-cycles",
		"/sched/goroutines:goroutines",
		"/sched/gomaxprocs:threads",
		"/no/such/metric:bytes",
	}
	s := make([]metrics.Sample, len(names))
	for i, n := range names {
		s[i].Name = n
	}
	metrics.Read(s)
	kinds := map[metrics.ValueKind]string{metrics.KindBad: "bad", metrics.KindUint64: "uint64", metrics.KindFloat64: "float64", metrics.KindFloat64Histogram: "histogram"}
	for _, x := range s {
		fmt.Println(x.Name, kinds[x.Value.Kind()])
	}
	for i := 0; i < 1000; i++ {
		sink = append(sink, make([]byte, 1024))
	}
	metrics.Read(s)
	fmt.Println("objects in use > 1000:", s[10].Value.Uint64() > 1000)
	fmt.Println("heap objects bytes > 1000 KB:", s[0].Value.Uint64() > 1000*1024)
	fmt.Println("goroutines >= 1:", s[13].Value.Uint64() >= 1)
	fmt.Println("gomaxprocs >= 1:", s[14].Value.Uint64() >= 1)
}

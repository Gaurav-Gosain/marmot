package runtime

import "unsafe"

// runtime/metrics.Read: the metrics marmot's runtime can tell, from the
// same numbers as ReadMemStats; any other name is KindBad, which Read's
// contract allows (a metric this runtime does not support).

// metricSample has the layout of runtime/metrics.Sample: the name, then a
// metrics.Value (kind, scalar, pointer).
type metricSample struct {
	name    string
	kind    int
	scalar  uint64
	pointer unsafe.Pointer
}

// The kinds of runtime/metrics.ValueKind.
const (
	metricKindBad = iota
	metricKindUint64
)

//go:linkname readMetrics runtime/metrics.runtime_readMetrics
func readMetrics(samplesp unsafe.Pointer, n int, _ int) {
	if n == 0 {
		return
	}
	var st rtMemstats
	rt_gc_stats(&st)
	samples := unsafe.Slice((*metricSample)(samplesp), n)
	for i := range samples {
		s := &samples[i]
		v, ok := uint64(0), true
		switch s.name {
		case "/memory/classes/heap/objects:bytes":
			v = st.heapAlloc
		case "/gc/heap/live:bytes":
			v = st.lastLive // marked live by the last GC: 0 before the first
		case "/memory/classes/heap/unused:bytes", "/memory/classes/heap/stacks:bytes":
			v = 0
		case "/memory/classes/heap/free:bytes":
			// Free and not returned to the OS.
			if st.heapSys > st.heapAlloc+st.heapReleased {
				v = st.heapSys - st.heapAlloc - st.heapReleased
			}
		case "/memory/classes/heap/released:bytes":
			v = st.heapReleased
		case "/memory/classes/total:bytes":
			v = st.heapSys
		case "/gc/heap/allocs:bytes":
			v = st.totalAlloc
		case "/gc/heap/allocs:objects":
			v = st.mallocs
		case "/gc/heap/frees:objects":
			v = st.frees
		case "/gc/heap/objects:objects":
			v = st.mallocs - st.frees
		case "/gc/cycles/total:gc-cycles", "/gc/cycles/automatic:gc-cycles":
			v = st.numGC
		case "/sched/goroutines:goroutines":
			v = uint64(NumGoroutine())
		case "/sched/gomaxprocs:threads":
			v = uint64(GOMAXPROCS(0))
		default:
			ok = false
		}
		// The pointer stays: gc leaves it for a caller's histogram.
		s.kind, s.scalar = metricKindBad, 0
		if ok {
			s.kind, s.scalar = metricKindUint64, v
		}
	}
}

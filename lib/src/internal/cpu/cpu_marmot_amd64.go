// marmot overlay of internal/cpu for amd64: features are not probed (the
// probes are assembly), so every optional feature reads as absent and the
// standard library takes its portable code paths. Untested.

package cpu

// CacheLinePadSize is used to prevent false sharing of cache lines.
const CacheLinePadSize = 64

func doinit() {}

// Name returns the CPU name, unknown under marmot.
func Name() string { return "" }

// DataCacheSizes returns the data cache sizes, unknown under marmot.
func DataCacheSizes() []uintptr { return nil }

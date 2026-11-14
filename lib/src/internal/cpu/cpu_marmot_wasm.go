// marmot overlay of internal/cpu for wasm: no features to probe, as gc's
// cpu_wasm.go.

package cpu

// CacheLinePadSize is used to prevent false sharing of cache lines.
const CacheLinePadSize = 64

func doinit() {}

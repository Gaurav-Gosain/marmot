// marmot overlay of internal/cpu for arm64: features are not probed (the
// probes are assembly), so every optional feature reads as absent and the
// standard library takes its portable code paths.

package cpu

// CacheLinePadSize is used to prevent false sharing of cache lines.
const CacheLinePadSize = 128

func doinit() {}

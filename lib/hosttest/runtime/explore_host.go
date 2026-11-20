// Host stand-ins for the schedule exploration hooks the pure files call
// (lib/src/runtime/explore_off.go without the bubble types): exploration is
// off on the host.

package runtime

const (
	exploreEnabled = false
	exploreActive  = false
)

func exploreYield(gp *g) {}

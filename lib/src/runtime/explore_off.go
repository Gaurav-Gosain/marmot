//go:build !marmotexplore

package runtime

// Without -tags marmotexplore there is no schedule exploration: the
// hooks behind exploreEnabled are dead code.

const (
	exploreEnabled = false
	exploreActive  = false
)

func exploreBubble(b *bubble)                         {}
func exploreBubbleDone(b *bubble)                     {}
func exploreNewMember(b *bubble, s *bubbleStart)      {}
func exploreStarted(gp *g, s *bubbleStart)            {}
func exploreYield(gp *g)                              {}
func exploreRelease(gp *g)                            {}
func exploreExit(gp *g)                               {}
func exploreArrive(gp *g)                             {}
func exploreReady(gp *g)                              {}
func exploreReport(b *bubble) string                  { return "" }

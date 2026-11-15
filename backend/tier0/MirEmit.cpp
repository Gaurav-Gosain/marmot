// MirEmit.cpp: see MirEmit.h.
#include "MirEmit.h"

namespace marmot {
namespace tier0 {
namespace mir {

void parallelMove(std::vector<Move> moves, MoveOps &ops) {
  std::vector<Move> pending;
  pending.reserve(moves.size());
  for (Move &m : moves)
    if (m.dst.k != Place::None && m.src.k != Place::None && !m.dst.same(m.src)) pending.push_back(m);
  while (!pending.empty()) {
    bool done = false;
    for (size_t i = 0; i < pending.size(); i++) {
      const Place &d = pending[i].dst;
      bool read = false;
      for (size_t j = 0; j < pending.size() && !read; j++)
        if (j != i && d.overlaps(pending[j].src)) read = true;
      if (read) continue;
      ops.move(d, pending[i].src);
      pending.erase(pending.begin() + long(i));
      done = true;
      break;
    }
    if (done) continue;
    // Every destination is still to be read: a cycle. Keep the first
    // destination's value in the scratch register; its readers read that.
    Place d = pending[0].dst;
    bool fp = false;
    for (const Move &m : pending)
      if (d.overlaps(m.src)) fp = m.src.fp;
    Place t = Place::reg_(ops.cycleScratch(fp), fp);
    Place from = d;
    from.fp = fp;
    ops.move(t, from);
    for (Move &m : pending)
      if (d.overlaps(m.src)) m.src = t;
  }
}

} // namespace mir
} // namespace tier0
} // namespace marmot

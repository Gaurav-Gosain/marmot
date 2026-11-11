// Random object graphs, single thread.
// expect-stdout: ok
// env: GOMAXPROCS=1
// timeout: 120
#include "gcgraph.h"

void marmot_main(void) {
  graph_stress(12345, 200, 2000, 5000);
  rt_memstats st;
  rt_gc_stats(&st);
  CHECK(st.num_gc > 3);
  out("ok\n");
}

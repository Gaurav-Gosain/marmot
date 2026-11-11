// MARMOT_GCSTRESS: collect every 50 allocations during graph mutation.
// expect-stdout: ok
// env: GOMAXPROCS=1 MARMOT_GCSTRESS=50
// timeout: 120
#include "gcgraph.h"

void marmot_main(void) {
  graph_stress(777, 20, 300, 1000);
  rt_memstats st;
  rt_gc_stats(&st);
  CHECK(st.num_gc > 100);
  out("ok\n");
}

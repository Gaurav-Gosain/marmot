// darwin, MARMOT_MADV_REUSE=1: a large object freed early gives its pages
// back (MADV_FREE_REUSABLE, phys_footprint drops). When the memory is
// allocated and written again, phys_footprint must count it again: the
// runtime calls MADV_FREE_REUSE first. Without that call the footprint reads
// low. Other systems: only the allocation round trip.
// expect-stdout: ok
// env: MARMOT_MADV_REUSE=1
#include "rtest.h"
#include <stdlib.h>
#ifdef __APPLE__
#include <mach/mach.h>
static uint64_t footprint(void) {
  task_vm_info_data_t vi;
  mach_msg_type_number_t c = TASK_VM_INFO_COUNT;
  if (task_info(mach_task_self(), TASK_VM_INFO, (task_info_t)&vi, &c) != KERN_SUCCESS) return 0;
  return vi.phys_footprint;
}
#endif

void marmot_main(void) {
  uintptr_t n = (uintptr_t)64 << 20;
  char *p = rt_alloc(n, RT_ALLOC_NOSCAN, 0, 0);
  memset(p, 1, n);
#ifdef __APPLE__
  uint64_t f1 = footprint();
#endif
  rt_free(p, n);  // 1 MiB or more: released at once
#ifdef __APPLE__
  uint64_t f2 = footprint();
  if (getenv("MADV_REUSE_DEBUG")) { outu(f1 >> 20); out(" "); outu(f2 >> 20); out("\n"); }
  CHECK(f2 + n / 2 < f1);
#endif
  char *q = rt_alloc(n, RT_ALLOC_NOSCAN, 0, 0);
  CHECK(q[0] == 0 && q[n - 1] == 0);
  memset(q, 2, n);
#ifdef __APPLE__
  uint64_t f3 = footprint();
  CHECK(f3 + n / 8 >= f1);
#endif
  out("ok\n");
}

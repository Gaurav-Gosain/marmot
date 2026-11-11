// stub.c: temporary single-goroutine implementation of the scheduler, timer
// and allocator entry points, until sched.c, timer.c, malloc.c and gc.c land.

#include "internal.h"

_Thread_local rt_m *rt_tls_m;
rt_m *getm(void) { return rt_tls_m; }
schedt sched;
rt_g *rt_main_g;
int64_t rt_gcstress;

static rt_g main_g;

void sched_init(void) { rt_main_g = &main_g; }
void timer_init(void) {}
void gc_init(void) {}

void sched_start_main(void) {
  main_g.status = G_RUNNING;
  marmot_main();
  rt_exit(0);
}

RT_API void rt_go(void (*fn)(void *), void *arg) { rt_throw("rt_go: not implemented yet"); }
RT_API rt_g *rt_current(void) { return &main_g; }
RT_API void rt_gosched(void) {}
RT_API void rt_park(void (*unlock)(void *), void *lock) {
  rt_throw("all goroutines are asleep - deadlock!");
}
RT_API void rt_ready(rt_g *g) { rt_throw("rt_ready: not implemented yet"); }
RT_API void rt_goexit(void) { rt_exit(0); }
RT_API void **rt_gslot(void) { return &main_g.slot; }
RT_API void **rt_gparam(rt_g *g) { return &g->param; }
RT_API intptr_t rt_numgoroutine(void) { return 1; }
RT_API int32_t rt_gomaxprocs(int32_t n) { return 1; }
RT_API int32_t rt_numcpu(void) { return os_ncpu(); }
RT_API void rt_entersyscall(void) {}
RT_API void rt_exitsyscall(void) {}

RT_API void rt_sleep(int64_t ns) {
  if (ns <= 0) return;
  int64_t end = os_nanotime() + ns;
  while (os_nanotime() < end) {
    int64_t left = end - os_nanotime();
    os_usleep(left > 1000000000 ? 1000000 : (uint32_t)(left / 1000 + 1));
  }
}
RT_API void *rt_timer_start(int64_t when, void (*fn)(void *), void *arg) {
  rt_throw("rt_timer_start: not implemented yet");
}
RT_API int32_t rt_timer_stop(void *t) { return 0; }

// Bump allocator over fresh mmap memory: zeroed, never freed.
static rt_spin heap_lock;
static uint8_t *heap_cur, *heap_end;
static rt_memstats stats;
static uint8_t zerobase[16];

void malloc_init(void) {}

RT_API void *rt_alloc(uintptr_t size, uint32_t kind, const uint8_t *ptrmap, uintptr_t elemsize) {
  if (size == 0) return zerobase;
  size = (size + 15) & ~(uintptr_t)15;
  if (size >= (1 << 20)) {
    void *p = os_map((size + rt_pagesize - 1) & ~(rt_pagesize - 1));
    if (!p) rt_throw("out of memory");
    stats.heap_alloc += size;
    stats.total_alloc += size;
    stats.mallocs++;
    return p;
  }
  spin_lock(&heap_lock);
  if (heap_cur + size > heap_end) {
    uintptr_t chunk = 16 << 20;
    heap_cur = os_map(chunk);
    if (!heap_cur) rt_throw("out of memory");
    heap_end = heap_cur + chunk;
    stats.heap_sys += chunk;
  }
  void *p = heap_cur;
  heap_cur += size;
  stats.heap_alloc += size;
  stats.total_alloc += size;
  stats.mallocs++;
  spin_unlock(&heap_lock);
  return p;
}

RT_API void rt_gc_collect(void) {}
RT_API void rt_gc_add_roots(void *start, void *end) {}
RT_API void rt_gc_set_finalizer(void *obj, void (*fn)(void *)) {}
RT_API void rt_gc_stats(rt_memstats *out) { *out = stats; }
RT_API int32_t rt_gc_set_percent(int32_t percent) { return 100; }

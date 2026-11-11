// stub.c: temporary allocator until malloc.c and gc.c land.

#include "internal.h"

int64_t rt_gcstress;
mcache *mcache_new(void) { return NULL; }
void gc_init(void) {}

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

// gcpar.c: opt-in parallel GC for programs with one goroutine. This object
// is linked only when something references rt_gc_parallel_enable (a driver
// passes -u); its constructor then enables the parallel collector.

#include "internal.h"

RT_API void rt_gc_parallel_enable(void) { gc_parallel_enable(); }

__attribute__((constructor)) static void gcpar_init(void) { gc_parallel_enable(); }

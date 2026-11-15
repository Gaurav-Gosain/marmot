// Package variable maps (frontend MARMOT_GCVARS=1): a variable with a
// record is scanned by its bits, so an address in a word whose bit is clear
// does not keep its object; one in a word whose bit is set does. Memory
// outside every record stays conservative.
// expect-stdout: ok
// env: GOMAXPROCS=1
// wasm: skip
#include "rtest.h"
#include <string.h>

#ifdef __APPLE__
#define GCVARS __attribute__((section("__DATA,__marmot_gcvars"), used))
#else
#define GCVARS __attribute__((section("marmot_gcvars"), used))
#endif

typedef struct {
  void *addr;
  uint64_t nwords;
  const uint8_t *bits;
} gcvar;

uintptr_t var[16];               // word 3 may hold a pointer, word 5 not
static const uint8_t var_bits[2] = {0x08, 0x00};
uintptr_t other;                 // no record: conservative
static const gcvar rec GCVARS = {var, 16, var_bits};

static uintptr_t *weak;  // NOSCAN memory: does not keep the objects alive

__attribute__((noinline)) static void setup(void) {
  weak = rt_alloc(3 * sizeof(uintptr_t), RT_ALLOC_NOSCAN, 0, 0);
  // Not the first object of its span (see gc_noptr.c).
  rt_alloc(64, RT_ALLOC_CONSERVATIVE, 0, 0);
  for (int i = 0; i < 3; i++) weak[i] = (uintptr_t)rt_alloc(64, RT_ALLOC_CONSERVATIVE, 0, 0);
  var[3] = weak[0];
  var[5] = weak[1];
  other = weak[2];
  for (int i = 0; i < 3; i++) rt_gc_weak_register(&weak[i]);
}

__attribute__((noinline)) static void scrub(void) {
  volatile char buf[16384];
  memset((char *)buf, 0, sizeof buf);
}

void marmot_main(void) {
  setup();
  scrub();
  rt_gc_collect();
  rt_gc_collect();
  CHECK(rt_gc_weak_load(&weak[0]) != NULL);  // bit set
  CHECK(rt_gc_weak_load(&weak[1]) == NULL);  // bit clear
  CHECK(rt_gc_weak_load(&weak[2]) != NULL);  // no record
  CHECK(var[5] != 0);
  out("ok\n");
}

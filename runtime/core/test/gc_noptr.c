// Package variables without pointers (frontend MARMOT_NOPTR=1: their own
// sections, test/noptr.ld on ELF) are not scanned: an address kept only in
// one of them does not keep its object alive; one kept in an ordinary
// variable does.
// expect-stdout: ok
// env: GOMAXPROCS=1
// wasm: skip
// ldflags-linux: -Wl,-T,test/noptr.ld
#include "rtest.h"
#include <string.h>

#ifdef __APPLE__
#define NOPTR_DATA __attribute__((section("__DATA,__noptrdata")))
#define NOPTR_BSS __attribute__((section("__DATA,__noptrbss,zerofill")))
#else
#define NOPTR_DATA __attribute__((section(".data.marmot.noptr.t_data")))
#define NOPTR_BSS __attribute__((section(".bss.marmot.noptr.t_bss")))
#endif

uintptr_t in_data NOPTR_DATA = 1;
uintptr_t in_bss[4096] NOPTR_BSS;
uintptr_t plain;  // an ordinary variable: scanned (not static: kept although never read)
static uintptr_t *weak;  // NOSCAN memory: does not keep the objects alive

__attribute__((noinline)) static void setup(void) {
  weak = rt_alloc(3 * sizeof(uintptr_t), RT_ALLOC_NOSCAN, 0, 0);
  // Not the first object of its span: the span's base address can stay in
  // a register or stack word the conservative scan finds.
  rt_alloc(64, RT_ALLOC_CONSERVATIVE, 0, 0);
  for (int i = 0; i < 3; i++) weak[i] = (uintptr_t)rt_alloc(64, RT_ALLOC_CONSERVATIVE, 0, 0);
  in_data = weak[0];
  in_bss[1000] = weak[1];
  plain = weak[2];
  for (int i = 0; i < 3; i++) rt_gc_weak_register(&weak[i]);
}

// Overwrites the stack below the caller, so no copy of the addresses
// setup made is left there for the conservative stack scan.
__attribute__((noinline)) static void scrub(void) {
  volatile char buf[16384];
  memset((char *)buf, 0, sizeof buf);
}

void marmot_main(void) {
  setup();
  scrub();
  rt_gc_collect();
  rt_gc_collect();
  CHECK(rt_gc_weak_load(&weak[2]) != NULL);  // kept by plain
  CHECK(rt_gc_weak_load(&weak[0]) == NULL);  // only in noptr data
  CHECK(rt_gc_weak_load(&weak[1]) == NULL);  // only in noptr bss
  CHECK(in_data != 0 && in_bss[1000] != 0);  // left alone
  out("ok\n");
}

// Stack maps (pc table version 4). The decoder on a table built here:
// lookups at sites, between sites, in a function without maps, outside the
// code, and on truncated tables. Then (darwin, where the table can live in
// a writable __MARMOT,__pcln section and be filled at run time) the scan:
// a frame suspended at a call with a map keeps only the objects its map
// marks; the other words of that frame are not scanned.
// expect-stdout: ok
// env: GOMAXPROCS=1
// wasm: skip (no frame chain)
#include "rtest.h"

typedef struct {
  uint8_t *b;
  uint32_t n;
} buf;

static void put32(uint8_t *p, uint32_t v) {
  p[0] = (uint8_t)v, p[1] = (uint8_t)(v >> 8), p[2] = (uint8_t)(v >> 16), p[3] = (uint8_t)(v >> 24);
}
static void var(buf *b, uint32_t v) {
  while (v >= 0x80) {
    b->b[b->n++] = (uint8_t)(v | 0x80);
    v >>= 7;
  }
  b->b[b->n++] = (uint8_t)v;
}

// A version 4 table: functions at starts[0..nf), text up to end; function
// mapfn has the given maps (nwords, bitmap bytes) and sites (offset, map id).
static uint32_t build(uint8_t *t, const uint32_t *starts, uint32_t nf, uint32_t end, uint32_t mapfn,
                      uint32_t nmaps, const uint32_t *nwords, const uint8_t *const *bits, uint32_t nsites,
                      const uint32_t *site_off, const uint32_t *site_map) {
  memset(t, 0, 84);
  memcpy(t, "MRMTPCLN", 8);
  put32(t + 8, 4);
  put32(t + 12, nf);
  put32(t + 28, 1);
  put32(t + 32, end);
  put32(t + 36, 84);
  buf b = {t, 84};
  for (uint32_t i = 0; i < nf; i++, b.n += 12) {
    memset(t + b.n, 0, 12);
    put32(t + b.n, starts[i]);
  }
  put32(t + 72, 1);
  put32(t + 76, b.n);
  put32(t + b.n, mapfn);
  put32(t + b.n + 4, 0);
  b.n += 8;
  put32(t + 80, b.n);
  var(&b, nmaps);
  for (uint32_t m = 0; m < nmaps; m++) {
    var(&b, nwords[m]);
    for (uint32_t k = 0; k < (nwords[m] + 7) / 8; k++) b.b[b.n++] = bits[m][k];
  }
  var(&b, nsites);
  uint32_t at = 0;
  for (uint32_t s = 0; s < nsites; s++) {
    var(&b, site_off[s] - at);
    at = site_off[s];
    var(&b, site_map[s]);
  }
  return b.n;
}

static void decoder(void) {
  static uint8_t t[512];
  const uint32_t starts[3] = {0x100, 0x200, 0x400};
  const uint8_t m0[2] = {0x05, 0x01}, m1[1] = {0x80};
  const uint8_t *bits[2] = {m0, m1};
  const uint32_t nw[2] = {9, 8};
  const uint32_t off[3] = {0x10, 0x24, 0x200};  // the last one is past 0x300: never reached
  const uint32_t map[3] = {0, 1, 0};
  uint32_t len = build(t, starts, 3, 0x500, 1, 2, nw, bits, 3, off, map);
  uintptr_t base = 0x10000;
  const uint8_t *got = 0;
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x210, &got) == 9 && got[0] == 0x05 && got[1] == 0x01);
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x224, &got) == 8 && got[0] == 0x80);
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x214, &got) == 0);  // between sites
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x200, &got) == 0);  // the function start
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x110, &got) == 0);  // a function without maps
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x410, &got) == 0);
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x50, &got) == 0);   // before the code
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x600, &got) == 0);  // after it
  CHECK(rt_pcln_stackmap(t, len, base, 0x10, &got) == 0);          // below the base
  for (uint32_t cut = 0; cut < len; cut++) {  // truncated: a map is whole or there is none
    uint32_t n = rt_pcln_stackmap(t, cut, base, base + 0x224, &got);
    CHECK(n == 0 || (n == 8 && got + 1 <= t + cut));
  }
  put32(t + 8, 3);  // version 3: no maps
  CHECK(rt_pcln_stackmap(t, len, base, base + 0x210, &got) == 0);
}

#if defined(__APPLE__)
__attribute__((used, section("__MARMOT,__pcln"))) static uint8_t live[512] = "MRMTPCLN";

#define NS 8
static uintptr_t *weak;  // in a no-scan block: the data segment is a conservative root
static int phase;
static uintptr_t image_base;

__attribute__((noinline)) static uintptr_t mkobj(void) {
  return (uintptr_t)rt_alloc(64, RT_ALLOC_NOSCAN, 0, 0);
}

// Overwrite the dead stack below the caller, where mkobj left copies.
__attribute__((noinline)) static void scrub(void) {
  volatile uintptr_t junk[512];
  for (int i = 0; i < 512; i++) junk[i] = 0;
}

static void holder(void);

__attribute__((noinline)) static void cb(volatile uintptr_t *slots) {
  if (phase++ == 0) {
    // The call site in holder (this frame's return address) gets a map
    // over holder's frame from slots[0] up to its fp: every word marked
    // except slots[0..5], whose objects are then kept by nothing.
    uintptr_t ret = (uintptr_t)__builtin_return_address(0);
    uintptr_t fp = (uintptr_t)__builtin_frame_address(1);
    uintptr_t lo = (uintptr_t)&slots[0];
    CHECK(fp > lo && (fp - lo) % 8 == 0 && fp - lo < 8 * 256);
    uint32_t nw = (uint32_t)((fp - lo) / 8);
    uint8_t bm[32];
    memset(bm, 0xff, sizeof bm);
    for (int i = 0; i < 6; i++) {
      uint32_t w = (uint32_t)((fp - (uintptr_t)&slots[i]) / 8 - 1);
      bm[w / 8] &= (uint8_t)~(1u << (w % 8));
    }
    const uint8_t *bits[1] = {bm};
    uint32_t start = (uint32_t)((uintptr_t)&holder - image_base);
    uint32_t site = (uint32_t)(ret - (uintptr_t)&holder);
    uint32_t starts[1] = {start}, nws[1] = {nw}, off[1] = {site}, map[1] = {0};
    CHECK(build(live, starts, 1, start + site + 4096, 0, 1, nws, bits, 1, off, map) <= sizeof live);
    return;
  }
  rt_gc_collect();
  int freed = 0;

  for (int i = 0; i < 6; i++) freed += weak[i] == 0;
  CHECK(weak[6] != 0 && weak[7] != 0);  // marked words keep theirs
  CHECK(freed >= 4);  // a copy may linger in a register or a runtime frame
}

__attribute__((noinline)) static void holder(void) {
  volatile uintptr_t slots[NS];
  for (int i = 0; i < NS; i++) {
    slots[i] = mkobj();
    weak[i] = slots[i];
    rt_gc_weak_register(&weak[i]);
  }
  scrub();
  for (int it = 0; it < 2; it++) cb(slots);  // one call site, twice
  for (int i = 0; i < NS; i++) rt_gc_weak_unregister(&weak[i]);
}

static void scan(void) {
  const uint8_t *t;
  intptr_t n;
  CHECK(rt_pcln(&t, &n, &image_base) == 1 && t == live);
  weak = rt_alloc(NS * sizeof *weak, RT_ALLOC_NOSCAN, 0, 0);
  holder();
}
#else
static void scan(void) {}
#endif

void marmot_main(void) {
  decoder();
  scan();
  out("ok\n");
}

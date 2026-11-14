// The save-area rule of the frame scan: a register value in a frame's save
// slot belongs to the nearest frame further up that saves that register,
// and the slot is skipped only when that owner is at a call site with a
// map. Frames O (no map, saves r21), M (mapped, saves r19) and F (saves r19
// and r21), F innermost: F's r19 slot is skipped (owner M), its r21 slot is
// scanned (owner O, which M passes through). A frame without save
// information owns every register.
// expect-stdout: ok
#include "rtest.h"
#include <stdbool.h>
#include <string.h>

extern void pcln_test_set(const uint8_t *t, intptr_t len, uintptr_t base);
extern void sched_test_scan_frames(uintptr_t lo, uintptr_t hi, uintptr_t fp, uintptr_t pc,
                                   void (*scan)(uintptr_t lo, uintptr_t hi));

static void put32(uint8_t *p, uint32_t v) {
  p[0] = (uint8_t)v, p[1] = (uint8_t)(v >> 8), p[2] = (uint8_t)(v >> 16), p[3] = (uint8_t)(v >> 24);
}
static uint8_t tab[512];
static uint32_t tn;
static void var(uint32_t v) {
  while (v >= 0x80) tab[tn++] = (uint8_t)(v | 0x80), v >>= 7;
  tab[tn++] = (uint8_t)v;
}

// Functions O, M, F at 0x100, 0x200, 0x300. saved[f] lists (reg, word)
// pairs; nsaved[f] RT_SAVED_UNKNOWN leaves function f without a blob.
static void build(const uint32_t *nsaved, const uint32_t (*saved)[2][2]) {
  memset(tab, 0, sizeof tab);
  memcpy(tab, "MRMTPCLN", 8);
  put32(tab + 8, 5);
  put32(tab + 12, 3);
  put32(tab + 28, 1);
  put32(tab + 32, 0x400);
  put32(tab + 36, 84);
  tn = 84;
  for (uint32_t f = 0; f < 3; f++, tn += 12) put32(tab + tn, 0x100 * (f + 1));
  uint32_t nb = 0, idx = tn;
  for (uint32_t f = 0; f < 3; f++) nb += nsaved[f] != RT_SAVED_UNKNOWN;
  put32(tab + 72, nb);
  put32(tab + 76, idx);
  tn += 8 * nb;
  uint32_t blobs = tn, k = 0;
  put32(tab + 80, blobs);
  for (uint32_t f = 0; f < 3; f++) {
    if (nsaved[f] == RT_SAVED_UNKNOWN) continue;
    put32(tab + idx + 8 * k, f);
    put32(tab + idx + 8 * k + 4, tn - blobs);
    k++;
    var(nsaved[f]);
    for (uint32_t j = 0; j < nsaved[f]; j++) var(saved[f][j][0]), var(saved[f][j][1]);
    if (f == 1) {  // M: one map of 4 words, none live, at its call site 0x10
      var(1), var(4), tab[tn++] = 0, var(1), var(0x10), var(0);
    } else {
      var(0), var(0);
    }
  }
}

static uintptr_t stack[64];
static int nscan;
static uintptr_t scanned[64][2];
static void rec(uintptr_t lo, uintptr_t hi) {
  scanned[nscan][0] = lo, scanned[nscan][1] = hi;
  nscan++;
}
static bool was_scanned(uintptr_t a) {
  for (int i = 0; i < nscan; i++)
    if (a >= scanned[i][0] && a + 8 <= scanned[i][1]) return true;
  return false;
}

// F at stack[8], M at stack[24], O at stack[40]; returns F's fp.
static uintptr_t frames(uintptr_t base) {
  memset(stack, 0, sizeof stack);
  uintptr_t fF = (uintptr_t)&stack[8], fM = (uintptr_t)&stack[24], fO = (uintptr_t)&stack[40];
  stack[8] = fM, stack[9] = base + 0x210;  // F's record: caller M, at M's site
  stack[24] = fO, stack[25] = base + 0x110;
  stack[40] = 0, stack[41] = 0;  // end of the chain
  return fF;
}

static void run(const uint32_t *ns, const uint32_t (*sv)[2][2], bool want19, bool want21) {
  uintptr_t base = 0x100000;
  build(ns, sv);
  pcln_test_set(tab, tn, base);
  uintptr_t fF = frames(base);
  nscan = 0;
  sched_test_scan_frames((uintptr_t)&stack[0], (uintptr_t)&stack[64], fF, base + 0x310, rec);
  // F saves r19 at word 0 (fp-8) and r21 at word 1 (fp-16).
  CHECK(was_scanned(fF - 8) == want19);
  CHECK(was_scanned(fF - 16) == want21);
}

void marmot_main(void) {
  static const uint32_t sv[3][2][2] = {{{21, 0}}, {{19, 0}}, {{19, 0}, {21, 1}}};
  // O saves r21, M (mapped) saves r19, F saves both.
  const uint32_t ns[3] = {1, 1, 2};
  run(ns, sv, false, true);
  // O unknown (no blob): r21 still owned by an unmapped frame.
  const uint32_t ns2[3] = {RT_SAVED_UNKNOWN, 1, 2};
  run(ns2, sv, false, true);
  // M without save information: it owns every register, unmapped as far as
  // the rule knows, so nothing in F is skipped.
  const uint32_t ns3[3] = {1, RT_SAVED_UNKNOWN, 2};
  run(ns3, sv, true, true);
  // M saves r19 and r21 itself: both of F's slots belong to M.
  static const uint32_t sv4[3][2][2] = {{{21, 0}}, {{19, 0}, {21, 1}}, {{19, 0}, {21, 1}}};
  const uint32_t ns4[3] = {1, 2, 2};
  run(ns4, sv4, false, false);
  out("ok\n");
}

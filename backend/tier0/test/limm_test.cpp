// limm_test.cpp: a64::logicalImm against an independent decoder, over random
// valid patterns (each must encode and decode back) and random values.
#include "Arm64.h"
#include <cstdio>
#include <random>
using namespace marmot::tier0::a64;
static uint64_t decode(uint32_t enc) {
  unsigned N = enc >> 12 & 1, immr = enc >> 6 & 63, imms = enc & 63;
  unsigned len = 31 - __builtin_clz((N << 6) | (~imms & 63));
  unsigned size = 1u << len, levels = size - 1;
  unsigned S = imms & levels, R = immr & levels;
  uint64_t welem = (S + 1 == 64) ? ~0ull : ((1ull << (S + 1)) - 1);
  uint64_t m = size == 64 ? ~0ull : (1ull << size) - 1;
  uint64_t elem = R ? (((welem >> R) | (welem << (size - R))) & m) : welem;
  uint64_t v = 0;
  for (unsigned i = 0; i < 64; i += size) v |= elem << i;
  return v;
}
int main() {
  std::mt19937_64 rng(1);
  int bad = 0, enc = 0;
  for (int t = 0; t < 2000000; t++) {
    uint64_t v;
    if (t % 2) {
      unsigned size = 2u << (rng() % 6), ones = 1 + rng() % (size - 1), rot = rng() % size;
      uint64_t m = size == 64 ? ~0ull : (1ull << size) - 1, e = ((ones == 64) ? ~0ull : (1ull << ones) - 1);
      e = rot ? (((e >> rot) | (e << (size - rot))) & m) : e;
      v = 0;
      for (unsigned i = 0; i < 64; i += size) v |= e << i;
    } else {
      v = rng();
    }
    uint32_t x;
    if (logicalImm(v, x)) {
      enc++;
      if (decode(x) != v) { if (bad++ < 5) printf("bad %016llx -> %03x -> %016llx\n", (unsigned long long)v, x, (unsigned long long)decode(x)); }
    } else if (t % 2) {
      if (bad++ < 5) printf("missed %016llx\n", (unsigned long long)v);
    }
  }
  printf("encoded %d, bad %d\n", enc, bad);
  return bad != 0;
}

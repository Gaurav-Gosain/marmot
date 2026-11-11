// cpu.c: rt_x86_level, the x86-64 psABI micro-architecture level of the
// running CPU, for loop multiversioning.

#include "internal.h"

#if RT_ARCH_AMD64
#include <cpuid.h>

static int32_t x86_level = -1;

static int32_t compute(void) {
  unsigned a, b, c, d;
  if (!__get_cpuid(1, &a, &b, &c, &d)) return 1;
  unsigned c1 = c;
  // v2: SSE3, SSSE3, SSE4.1, SSE4.2, POPCNT, CMPXCHG16B; LAHF-SAHF below.
  bool v2 = (c1 & (1u << 0)) && (c1 & (1u << 9)) && (c1 & (1u << 19)) && (c1 & (1u << 20)) &&
            (c1 & (1u << 23)) && (c1 & (1u << 13));
  unsigned e1c = 0;
  if (__get_cpuid(0x80000001, &a, &b, &c, &d)) e1c = c;
  v2 = v2 && (e1c & 1u);
  if (!v2) return 1;
  // v3: AVX, FMA, F16C, MOVBE, OSXSAVE with XMM+YMM state enabled; AVX2,
  // BMI1, BMI2 (leaf 7); LZCNT (ABM, 0x80000001 ecx bit 5).
  bool osx = c1 & (1u << 27);
  uint64_t xcr0 = 0;
  if (osx) {
    unsigned lo, hi;
    __asm__ volatile("xgetbv" : "=a"(lo), "=d"(hi) : "c"(0));
    xcr0 = ((uint64_t)hi << 32) | lo;
  }
  bool ymm = (xcr0 & 6) == 6;
  unsigned b7 = 0;
  if (__get_cpuid_count(7, 0, &a, &b, &c, &d)) b7 = b;
  bool v3 = ymm && (c1 & (1u << 28)) && (c1 & (1u << 12)) && (c1 & (1u << 29)) && (c1 & (1u << 22)) &&
            (b7 & (1u << 5)) && (b7 & (1u << 3)) && (b7 & (1u << 8)) && (e1c & (1u << 5));
  if (!v3) return 2;
  // v4: AVX512 F, DQ, CD, BW, VL with opmask and ZMM state enabled.
  bool zmm = (xcr0 & 0xe0) == 0xe0;
  bool v4 = zmm && (b7 & (1u << 16)) && (b7 & (1u << 17)) && (b7 & (1u << 28)) && (b7 & (1u << 30)) &&
            (b7 & (1u << 31));
  return v4 ? 4 : 3;
}

RT_API int32_t rt_x86_level(void) {
  int32_t l = __atomic_load_n(&x86_level, __ATOMIC_RELAXED);
  if (UNLIKELY(l < 0)) {
    l = compute();
    __atomic_store_n(&x86_level, l, __ATOMIC_RELAXED);
  }
  return l;
}
#else
RT_API int32_t rt_x86_level(void) { return 0; }
#endif

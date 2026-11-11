// simd.c: vectorized byte and string primitives for internal/bytealg,
// unicode/utf8 and map hashing. NEON on arm64, SSE2 on x86-64 (baseline, no
// dispatch needed), scalar elsewhere. Semantics match the Go functions named
// in rt.h exactly. Loads never cross the end of the buffer.
//
// These functions touch no static data, so the backend can inline them from
// rtcore.bc.

#include "internal.h"

#if defined(__aarch64__)
#include <arm_neon.h>
#define RT_NEON 1
#elif defined(__x86_64__)
#include <emmintrin.h>
#define RT_SSE2 1
#endif

typedef const uint8_t *bp;

INLINE uint64_t load64(bp p) {
  uint64_t v;
  __builtin_memcpy(&v, p, 8);
  return v;
}

INLINE uint32_t load32(bp p) {
  uint32_t v;
  __builtin_memcpy(&v, p, 4);
  return v;
}

// ---- 16-byte block helpers: bit i of the mask is set if byte i matched ----

#if RT_NEON
// 4 bits per byte (shrn trick); divide the bit index by 4.
INLINE uint64_t eq_mask16(bp p, uint8x16_t c) {
  uint8x16_t eq = vceqq_u8(vld1q_u8(p), c);
  return vget_lane_u64(vreinterpret_u64_u8(vshrn_n_u16(vreinterpretq_u16_u8(eq), 4)), 0);
}
#define MASK_SHIFT 2
#elif RT_SSE2
INLINE uint64_t eq_mask16(bp p, __m128i c) {
  return (uint64_t)_mm_movemask_epi8(_mm_cmpeq_epi8(_mm_loadu_si128((const __m128i *)p), c));
}
#define MASK_SHIFT 0
#endif

// ---- IndexByte, LastIndexByte, Count ----

RT_API intptr_t rt_index_byte(const void *s, intptr_t n, uint8_t c) {
  bp p = s;
  intptr_t i = 0;
#if RT_NEON || RT_SSE2
  if (n >= 16) {
#if RT_NEON
    uint8x16_t vc = vdupq_n_u8(c);
    for (; i + 64 <= n; i += 64) {
      uint8x16_t a = vceqq_u8(vld1q_u8(p + i), vc), b = vceqq_u8(vld1q_u8(p + i + 16), vc);
      uint8x16_t c2 = vceqq_u8(vld1q_u8(p + i + 32), vc), d = vceqq_u8(vld1q_u8(p + i + 48), vc);
      if (vmaxvq_u8(vorrq_u8(vorrq_u8(a, b), vorrq_u8(c2, d)))) break;
    }
#else
    __m128i vc = _mm_set1_epi8((char)c);
#endif
    for (; i + 16 <= n; i += 16) {
      uint64_t m = eq_mask16(p + i, vc);
      if (m) return i + (intptr_t)(__builtin_ctzll(m) >> MASK_SHIFT);
    }
    if (i < n) {
      // Overlapping last block.
      uint64_t m = eq_mask16(p + n - 16, vc);
      if (m) return n - 16 + (intptr_t)(__builtin_ctzll(m) >> MASK_SHIFT);
    }
    return -1;
  }
#endif
  for (; i < n; i++)
    if (p[i] == c) return i;
  return -1;
}

RT_API intptr_t rt_last_index_byte(const void *s, intptr_t n, uint8_t c) {
  bp p = s;
  intptr_t i = n;
#if RT_NEON || RT_SSE2
  if (n >= 16) {
#if RT_NEON
    uint8x16_t vc = vdupq_n_u8(c);
#else
    __m128i vc = _mm_set1_epi8((char)c);
#endif
    for (; i >= 16; i -= 16) {
      uint64_t m = eq_mask16(p + i - 16, vc);
      if (m) return i - 16 + (intptr_t)((63 - __builtin_clzll(m)) >> MASK_SHIFT);
    }
    if (i > 0) {
      uint64_t m = eq_mask16(p, vc);
      if (m) return (intptr_t)((63 - __builtin_clzll(m)) >> MASK_SHIFT);
    }
    return -1;
  }
#endif
  while (i > 0)
    if (p[--i] == c) return i;
  return -1;
}

RT_API intptr_t rt_count_byte(const void *s, intptr_t n, uint8_t c) {
  bp p = s;
  intptr_t i = 0, count = 0;
#if RT_NEON
  uint8x16_t vc = vdupq_n_u8(c);
  while (i + 16 <= n) {
    // Accumulate up to 255 blocks in 8-bit lanes (each match adds 1).
    intptr_t blocks = (n - i) / 16;
    if (blocks > 255) blocks = 255;
    uint8x16_t acc = vdupq_n_u8(0);
    for (intptr_t k = 0; k < blocks; k++, i += 16) acc = vsubq_u8(acc, vceqq_u8(vld1q_u8(p + i), vc));
    count += vaddlvq_u8(acc);
  }
#elif RT_SSE2
  __m128i vc = _mm_set1_epi8((char)c);
  while (i + 16 <= n) {
    intptr_t blocks = (n - i) / 16;
    if (blocks > 255) blocks = 255;
    __m128i acc = _mm_setzero_si128();
    for (intptr_t k = 0; k < blocks; k++, i += 16)
      acc = _mm_sub_epi8(acc, _mm_cmpeq_epi8(_mm_loadu_si128((const __m128i *)(p + i)), vc));
    __m128i sums = _mm_sad_epu8(acc, _mm_setzero_si128());
    count += _mm_cvtsi128_si32(sums) + _mm_extract_epi16(sums, 4);
  }
#endif
  for (; i < n; i++) count += p[i] == c;
  return count;
}

// ---- Equal, Compare ----

RT_API int32_t rt_mem_equal(const void *a, const void *b, intptr_t n) {
  bp x = a, y = b;
  if (n >= 8) {
    if (n <= 16) return (int32_t)(load64(x) == load64(y)) & (int32_t)(load64(x + n - 8) == load64(y + n - 8));
    return __builtin_memcmp(x, y, (size_t)n) == 0;
  }
  if (n >= 4) return (int32_t)(load32(x) == load32(y)) & (int32_t)(load32(x + n - 4) == load32(y + n - 4));
  for (intptr_t i = 0; i < n; i++)
    if (x[i] != y[i]) return 0;
  return 1;
}

RT_API int32_t rt_compare(const void *a, intptr_t an, const void *b, intptr_t bn) {
  intptr_t n = an < bn ? an : bn;
  if (a != b && n > 0) {
    int r = __builtin_memcmp(a, b, (size_t)n);
    if (r) return r < 0 ? -1 : 1;
  }
  return an < bn ? -1 : (an > bn ? 1 : 0);
}

// ---- Index (substring) ----

// First and last byte filter (Mula), then compare the middle.
RT_API intptr_t rt_index(const void *s, intptr_t n, const void *sep, intptr_t m) {
  bp p = s, q = sep;
  if (m == 0) return 0;
  if (m > n) return -1;
  if (m == 1) return rt_index_byte(p, n, q[0]);
  intptr_t last = n - m;  // last valid start
  intptr_t i = 0;
#if RT_NEON || RT_SSE2
#if RT_NEON
  uint8x16_t f = vdupq_n_u8(q[0]), l = vdupq_n_u8(q[m - 1]);
#else
  __m128i f = _mm_set1_epi8((char)q[0]), l = _mm_set1_epi8((char)q[m - 1]);
#endif
  for (; i + 16 <= last + 1; i += 16) {
    uint64_t mk = eq_mask16(p + i, f) & eq_mask16(p + i + m - 1, l);
    while (mk) {
      intptr_t j = i + (intptr_t)(__builtin_ctzll(mk) >> MASK_SHIFT);
      if (rt_mem_equal(p + j + 1, q + 1, m - 2)) return j;
#if RT_NEON
      mk &= ~(0xfull << ((__builtin_ctzll(mk) >> 2) << 2));
#else
      mk &= mk - 1;
#endif
    }
  }
#endif
  for (; i <= last; i++)
    if (p[i] == q[0] && p[i + m - 1] == q[m - 1] && rt_mem_equal(p + i + 1, q + 1, m - 2)) return i;
  return -1;
}

// ---- UTF-8 ----

// Length of the ASCII prefix.
RT_API intptr_t rt_ascii_prefix(const void *s, intptr_t n) {
  bp p = s;
  intptr_t i = 0;
#if RT_NEON
  for (; i + 64 <= n; i += 64) {
    uint8x16_t v = vorrq_u8(vorrq_u8(vld1q_u8(p + i), vld1q_u8(p + i + 16)),
                            vorrq_u8(vld1q_u8(p + i + 32), vld1q_u8(p + i + 48)));
    if (vmaxvq_u8(v) >= 0x80) break;
  }
  for (; i + 16 <= n; i += 16) {
    uint8x16_t v = vld1q_u8(p + i);
    if (vmaxvq_u8(v) >= 0x80) break;
  }
#elif RT_SSE2
  for (; i + 16 <= n; i += 16) {
    if (_mm_movemask_epi8(_mm_loadu_si128((const __m128i *)(p + i)))) break;
  }
#endif
  for (; i + 8 <= n; i += 8)
    if (load64(p + i) & 0x8080808080808080ull) break;
  for (; i < n; i++)
    if (p[i] >= 0x80) break;
  return i;
}

// Decode one non-ASCII sequence at p[0..n). Returns its length if valid,
// 0 if invalid (Go treats an invalid byte as a 1-byte RuneError).
INLINE intptr_t utf8_seq(bp p, intptr_t n) {
  uint8_t b0 = p[0];
  if (b0 < 0xC2 || b0 > 0xF4) return 0;
  if (b0 < 0xE0) {
    if (n < 2 || (p[1] & 0xC0) != 0x80) return 0;
    return 2;
  }
  // Allowed range for the second byte (Go's acceptRanges).
  uint8_t lo = 0x80, hi = 0xBF;
  if (b0 == 0xE0) lo = 0xA0;
  else if (b0 == 0xED) hi = 0x9F;
  else if (b0 == 0xF0) lo = 0x90;
  else if (b0 == 0xF4) hi = 0x8F;
  if (n < 2 || p[1] < lo || p[1] > hi) return 0;
  if (n < 3 || (p[2] & 0xC0) != 0x80) return 0;
  if (b0 < 0xF0) return 3;
  if (n < 4 || (p[3] & 0xC0) != 0x80) return 0;
  return 4;
}

#if RT_NEON
// Keiser and Lemire, "Validating UTF-8 In Less Than One Instruction Per Byte"
// (lookup algorithm). Classifies each byte from its high nibble, the
// previous byte's nibbles, and checks continuation bytes for 3 and 4 byte
// sequences. Equivalent to Go's utf8.Valid (rejects overlongs, surrogates,
// values above U+10FFFF, truncation).
enum {
  TOO_SHORT = 1 << 0, TOO_LONG = 1 << 1, OVERLONG_3 = 1 << 2, TOO_LARGE = 1 << 3,
  SURROGATE = 1 << 4, OVERLONG_2 = 1 << 5, TOO_LARGE_1000 = 1 << 6, OVERLONG_4 = 1 << 6,
  TWO_CONTS = 1 << 7, CARRY = TOO_SHORT | TOO_LONG | TWO_CONTS,
};

INLINE uint8x16_t utf8_check_block(uint8x16_t input, uint8x16_t prev) {
  const uint8x16_t t1 = {TOO_LONG, TOO_LONG, TOO_LONG, TOO_LONG, TOO_LONG, TOO_LONG, TOO_LONG,
                         TOO_LONG, TWO_CONTS, TWO_CONTS, TWO_CONTS, TWO_CONTS,
                         TOO_SHORT | OVERLONG_2, TOO_SHORT, TOO_SHORT | OVERLONG_3 | SURROGATE,
                         TOO_SHORT | TOO_LARGE | TOO_LARGE_1000 | OVERLONG_4};
  const uint8x16_t t2 = {CARRY | OVERLONG_3 | OVERLONG_2 | OVERLONG_4, CARRY | OVERLONG_2, CARRY,
                         CARRY, CARRY | TOO_LARGE, CARRY | TOO_LARGE | TOO_LARGE_1000,
                         CARRY | TOO_LARGE | TOO_LARGE_1000, CARRY | TOO_LARGE | TOO_LARGE_1000,
                         CARRY | TOO_LARGE | TOO_LARGE_1000, CARRY | TOO_LARGE | TOO_LARGE_1000,
                         CARRY | TOO_LARGE | TOO_LARGE_1000, CARRY | TOO_LARGE | TOO_LARGE_1000,
                         CARRY | TOO_LARGE | TOO_LARGE_1000,
                         CARRY | TOO_LARGE | TOO_LARGE_1000 | SURROGATE,
                         CARRY | TOO_LARGE | TOO_LARGE_1000, CARRY | TOO_LARGE | TOO_LARGE_1000};
  const uint8x16_t t3 = {TOO_SHORT, TOO_SHORT, TOO_SHORT, TOO_SHORT, TOO_SHORT, TOO_SHORT,
                         TOO_SHORT, TOO_SHORT,
                         TOO_LONG | OVERLONG_2 | TWO_CONTS | OVERLONG_3 | TOO_LARGE_1000 | OVERLONG_4,
                         TOO_LONG | OVERLONG_2 | TWO_CONTS | OVERLONG_3 | TOO_LARGE,
                         TOO_LONG | OVERLONG_2 | TWO_CONTS | SURROGATE | TOO_LARGE,
                         TOO_LONG | OVERLONG_2 | TWO_CONTS | SURROGATE | TOO_LARGE,
                         TOO_SHORT, TOO_SHORT, TOO_SHORT, TOO_SHORT};
  uint8x16_t prev1 = vextq_u8(prev, input, 15);
  uint8x16_t sc = vandq_u8(vandq_u8(vqtbl1q_u8(t1, vshrq_n_u8(prev1, 4)),
                                    vqtbl1q_u8(t2, vandq_u8(prev1, vdupq_n_u8(0x0F)))),
                           vqtbl1q_u8(t3, vshrq_n_u8(input, 4)));
  uint8x16_t prev2 = vextq_u8(prev, input, 14);
  uint8x16_t prev3 = vextq_u8(prev, input, 13);
  uint8x16_t must23 = vorrq_u8(vcgeq_u8(prev2, vdupq_n_u8(0xE0)), vcgeq_u8(prev3, vdupq_n_u8(0xF0)));
  return veorq_u8(vandq_u8(must23, vdupq_n_u8(0x80)), sc);
}

// Nonzero lanes where the block ends inside a multibyte sequence.
INLINE uint8x16_t utf8_incomplete(uint8x16_t input) {
  const uint8x16_t max = {255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
                          0xF0 - 1, 0xE0 - 1, 0xC0 - 1};
  return vqsubq_u8(input, max);
}

// Validate 16-byte blocks of p[0..n16), n16 a multiple of 16. Returns 1 if
// valid so far; *prevp and *incp carry state to the tail.
static int32_t utf8_valid_neon(bp p, intptr_t n16, uint8x16_t *prevp, uint8x16_t *incp) {
  uint8x16_t prev = vdupq_n_u8(0), err = vdupq_n_u8(0), inc = vdupq_n_u8(0);
  intptr_t i = 0;
  for (; i + 64 <= n16; i += 64) {
    uint8x16_t a = vld1q_u8(p + i), b = vld1q_u8(p + i + 16);
    uint8x16_t c = vld1q_u8(p + i + 32), d = vld1q_u8(p + i + 48);
    if (vmaxvq_u8(vorrq_u8(vorrq_u8(a, b), vorrq_u8(c, d))) < 0x80) {
      // All ASCII: only a sequence left open before this chunk is an error.
      err = vorrq_u8(err, inc);
      inc = vdupq_n_u8(0);
      prev = d;
      continue;
    }
    err = vorrq_u8(err, utf8_check_block(a, prev));
    err = vorrq_u8(err, utf8_check_block(b, a));
    err = vorrq_u8(err, utf8_check_block(c, b));
    err = vorrq_u8(err, utf8_check_block(d, c));
    inc = utf8_incomplete(d);
    prev = d;
    if ((i & 1023) == 960 && vmaxvq_u8(err)) return 0;
  }
  for (; i < n16; i += 16) {
    uint8x16_t in = vld1q_u8(p + i);
    if (vmaxvq_u8(in) < 0x80) {
      err = vorrq_u8(err, inc);
    } else {
      err = vorrq_u8(err, utf8_check_block(in, prev));
      inc = utf8_incomplete(in);
    }
    prev = in;
    if ((i & 1023) == 1008 && vmaxvq_u8(err)) return 0;
  }
  *prevp = prev;
  *incp = inc;
  return vmaxvq_u8(err) == 0;
}
#endif

// utf8.Valid.
RT_API int32_t rt_utf8_valid(const void *s, intptr_t n) {
  bp p = s;
  // An ASCII prefix is valid on its own and no sequence starts inside it.
  intptr_t i = rt_ascii_prefix(p, n);
  if (i == n) return 1;
  p += i;
  n -= i;
  i = 0;
#if RT_NEON
  if (n >= 64) {
    intptr_t n16 = n & ~(intptr_t)15;
    uint8x16_t prev, inc;
    if (!utf8_valid_neon(p, n16, &prev, &inc)) return 0;
    if (n16 == n) return vmaxvq_u8(inc) == 0;
    // Tail: pad with zeros (ASCII) in a local block.
    uint8_t tail[16] = {0};
    __builtin_memcpy(tail, p + n16, (size_t)(n - n16));
    uint8x16_t in = vld1q_u8(tail);
    uint8x16_t err = vorrq_u8(utf8_check_block(in, prev), utf8_incomplete(in));
    return vmaxvq_u8(err) == 0;
  }
#endif
  while (i < n) {
    i += rt_ascii_prefix(p + i, n - i);
    if (i >= n) break;
    intptr_t k = utf8_seq(p + i, n - i);
    if (!k) return 0;
    i += k;
  }
  return 1;
}

// utf8.RuneCount: invalid bytes count as one rune each.
RT_API intptr_t rt_utf8_count(const void *s, intptr_t n) {
  bp p = s;
  intptr_t i = rt_ascii_prefix(p, n), runes = i;
  if (i == n) return n;
#if RT_NEON
  // Valid text after the ASCII prefix: runes are the bytes that are not
  // continuation bytes.
  if (n - i >= 64 && rt_utf8_valid(p + i, n - i)) {
    uint8x16_t c0 = vdupq_n_u8(0xC0), cont = vdupq_n_u8(0x80);
    intptr_t start = i, conts = 0;
    while (i + 16 <= n) {
      intptr_t blocks = (n - i) / 16;
      if (blocks > 255) blocks = 255;
      uint8x16_t acc = vdupq_n_u8(0);
      for (intptr_t k = 0; k < blocks; k++, i += 16)
        acc = vsubq_u8(acc, vceqq_u8(vandq_u8(vld1q_u8(p + i), c0), cont));
      conts += vaddlvq_u8(acc);
    }
    for (; i < n; i++) conts += (p[i] & 0xC0) == 0x80;
    return runes + (n - start) - conts;
  }
#endif
  while (i < n) {
    intptr_t a = rt_ascii_prefix(p + i, n - i);
    i += a;
    runes += a;
    if (i >= n) break;
    intptr_t k = utf8_seq(p + i, n - i);
    i += k ? k : 1;
    runes++;
  }
  return runes;
}

// ---- hashing (wyhash, final version 4) ----

INLINE void wymum(uint64_t *a, uint64_t *b) {
  __uint128_t r = (__uint128_t)*a * *b;
  *a = (uint64_t)r;
  *b = (uint64_t)(r >> 64);
}

INLINE uint64_t wymix(uint64_t a, uint64_t b) {
  wymum(&a, &b);
  return a ^ b;
}

INLINE uint64_t wyr3(bp p, size_t k) {
  return (((uint64_t)p[0]) << 16) | (((uint64_t)p[k >> 1]) << 8) | p[k - 1];
}

#define WYP0 0x2d358dccaa6c78a5ull
#define WYP1 0x8bb84b93962eacc9ull
#define WYP2 0x4b33a62ed433d4a3ull
#define WYP3 0x4d5a2da51de1aa47ull

RT_API uint64_t rt_hash_bytes(const void *key, intptr_t len, uint64_t seed) {
  bp p = key;
  size_t n = (size_t)len;
  seed ^= wymix(seed ^ WYP0, WYP1);
  uint64_t a, b;
  if (n <= 16) {
    if (n >= 4) {
      a = ((uint64_t)load32(p) << 32) | load32(p + ((n >> 3) << 2));
      b = ((uint64_t)load32(p + n - 4) << 32) | load32(p + n - 4 - ((n >> 3) << 2));
    } else if (n > 0) {
      a = wyr3(p, n);
      b = 0;
    } else {
      a = b = 0;
    }
  } else {
    size_t i = n;
    if (i > 48) {
      uint64_t see1 = seed, see2 = seed;
      do {
        seed = wymix(load64(p) ^ WYP1, load64(p + 8) ^ seed);
        see1 = wymix(load64(p + 16) ^ WYP2, load64(p + 24) ^ see1);
        see2 = wymix(load64(p + 32) ^ WYP3, load64(p + 40) ^ see2);
        p += 48;
        i -= 48;
      } while (i > 48);
      seed ^= see1 ^ see2;
    }
    while (i > 16) {
      seed = wymix(load64(p) ^ WYP1, load64(p + 8) ^ seed);
      i -= 16;
      p += 16;
    }
    a = load64(p + i - 16);
    b = load64(p + i - 8);
  }
  a ^= WYP1;
  b ^= seed;
  wymum(&a, &b);
  return wymix(a ^ WYP0 ^ n, b ^ WYP1);
}

RT_API uint64_t rt_hash_u64(uint64_t x, uint64_t seed) {
  return wymix(x ^ WYP0 ^ seed, (x >> 32 | x << 32) ^ WYP1);
}

// ---- decimal digits and hex ----

// Value of 8 ASCII digits at p (SWAR, Lemire).
INLINE uint64_t digits8(uint64_t v) {
  v -= 0x3030303030303030ull;
  v = (v * 10) + (v >> 8);
  v = (((v & 0x000000FF000000FFull) * (100 + (1000000ull << 32))) +
       (((v >> 16) & 0x000000FF000000FFull) * (1 + (10000ull << 32)))) >> 32;
  return v;
}

// Mask with 0x80 in every byte of v that is not an ASCII digit.
INLINE uint64_t nondigit_mask(uint64_t v) {
  return ((v + 0x4646464646464646ull) | (v - 0x3030303030303030ull)) & 0x8080808080808080ull;
}

// Parse the run of ASCII decimal digits at p[0..n), at most 19 digits (so
// no overflow). Returns the number of digits consumed (0 if p[0] is not a
// digit) and stores their value in *out.
RT_API intptr_t rt_parse_digits(const void *s, intptr_t n, uint64_t *out) {
  bp p = s;
  if (n > 19) n = 19;
  uint64_t v = 0;
  intptr_t i = 0;
  while (i + 8 <= n) {
    uint64_t w = load64(p + i);
    uint64_t m = nondigit_mask(w);
    if (m) {
      intptr_t k = (intptr_t)(__builtin_ctzll(m) >> 3);  // digits before the first non-digit
      for (intptr_t j = 0; j < k; j++) v = v * 10 + (uint64_t)(p[i + j] - '0');
      *out = v;
      return i + k;
    }
    v = v * 100000000ull + digits8(w);
    i += 8;
  }
  for (; i < n; i++) {
    unsigned d = (unsigned)p[i] - '0';
    if (d > 9) break;
    v = v * 10 + d;
  }
  *out = v;
  return i;
}

// Lowercase hex of src[0..n) into dst[0..2n).
RT_API void rt_hex_encode(uint8_t *dst, const void *src, intptr_t n) {
  bp s = src;
  intptr_t i = 0;
#if RT_NEON
  const uint8x16_t tab = {'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'};
  for (; i + 16 <= n; i += 16) {
    uint8x16_t v = vld1q_u8(s + i);
    uint8x16_t hi = vqtbl1q_u8(tab, vshrq_n_u8(v, 4)), lo = vqtbl1q_u8(tab, vandq_u8(v, vdupq_n_u8(15)));
    vst1q_u8(dst + 2 * i, vzip1q_u8(hi, lo));
    vst1q_u8(dst + 2 * i + 16, vzip2q_u8(hi, lo));
  }
#endif
  // No string literal: it would be a static global and block inlining
  // from rtcore.bc.
  for (; i < n; i++) {
    uint8_t h = s[i] >> 4, l = s[i] & 15;
    dst[2 * i] = (uint8_t)(h < 10 ? '0' + h : 'a' - 10 + h);
    dst[2 * i + 1] = (uint8_t)(l < 10 ? '0' + l : 'a' - 10 + l);
  }
}

INLINE int hexval(uint8_t c) {
  if (c >= '0' && c <= '9') return c - '0';
  c |= 0x20;
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  return -1;
}

// Decode hex src[0..2n) into dst[0..n). Returns n, or -(i+1) where i is the
// index in src of the first invalid character (encoding/hex reports it).
RT_API intptr_t rt_hex_decode(uint8_t *dst, const void *src, intptr_t n) {
  bp s = src;
  intptr_t i = 0;
#if RT_NEON
  for (; i + 16 <= n; i += 16) {
    uint8x16x2_t in = vld2q_u8(s + 2 * i);  // even (high) and odd (low) characters
    uint8x16_t vals[2];
    bool bad = false;
    for (int k = 0; k < 2; k++) {
      uint8x16_t c = in.val[k];
      uint8x16_t d = vsubq_u8(c, vdupq_n_u8('0'));
      uint8x16_t isd = vcltq_u8(d, vdupq_n_u8(10));
      uint8x16_t l = vsubq_u8(vorrq_u8(c, vdupq_n_u8(0x20)), vdupq_n_u8('a'));
      uint8x16_t isl = vcltq_u8(l, vdupq_n_u8(6));
      if (vminvq_u8(vorrq_u8(isd, isl)) == 0) bad = true;
      vals[k] = vbslq_u8(isd, d, vaddq_u8(l, vdupq_n_u8(10)));
    }
    if (bad) break;  // the scalar loop finds the exact position
    vst1q_u8(dst + i, vorrq_u8(vshlq_n_u8(vals[0], 4), vals[1]));
  }
#endif
  for (; i < n; i++) {
    int a = hexval(s[2 * i]), b = hexval(s[2 * i + 1]);
    if (a < 0) return -(2 * i + 1);
    if (b < 0) return -(2 * i + 2);
    dst[i] = (uint8_t)(a << 4 | b);
  }
  return n;
}

// ---- base64 ----

INLINE uint8_t b64_char(uint8_t v, int url) {
  // A-Z a-z 0-9 then +/ or -_, computed (no table: keeps this inlinable).
  if (v < 26) return (uint8_t)('A' + v);
  if (v < 52) return (uint8_t)('a' + v - 26);
  if (v < 62) return (uint8_t)('0' + v - 52);
  return v == 62 ? (url ? '-' : '+') : (url ? '_' : '/');
}

INLINE int b64_val(uint8_t c, int url) {
  if (c >= 'A' && c <= 'Z') return c - 'A';
  if (c >= 'a' && c <= 'z') return c - 'a' + 26;
  if (c >= '0' && c <= '9') return c - '0' + 52;
  if (c == (url ? '-' : '+')) return 62;
  if (c == (url ? '_' : '/')) return 63;
  return -1;
}

#if RT_NEON
// Map 6-bit values to characters arithmetically: add an offset chosen by range.
INLINE uint8x16_t b64_enc_vec(uint8x16_t v, int url) {
  uint8x16_t off = vdupq_n_u8('A');
  off = vbslq_u8(vcgeq_u8(v, vdupq_n_u8(26)), vdupq_n_u8((uint8_t)('a' - 26)), off);
  off = vbslq_u8(vcgeq_u8(v, vdupq_n_u8(52)), vdupq_n_u8((uint8_t)('0' - 52)), off);
  off = vbslq_u8(vceqq_u8(v, vdupq_n_u8(62)), vdupq_n_u8((uint8_t)((url ? '-' : '+') - 62)), off);
  off = vbslq_u8(vceqq_u8(v, vdupq_n_u8(63)), vdupq_n_u8((uint8_t)((url ? '_' : '/') - 63)), off);
  return vaddq_u8(v, off);
}

// Characters to 6-bit values; 0xFF lanes for invalid characters.
INLINE uint8x16_t b64_dec_vec(uint8x16_t c, int url) {
  uint8x16_t r = vdupq_n_u8(0xFF);
  uint8x16_t up = vsubq_u8(c, vdupq_n_u8('A'));
  r = vbslq_u8(vcltq_u8(up, vdupq_n_u8(26)), up, r);
  uint8x16_t lo = vsubq_u8(c, vdupq_n_u8('a'));
  r = vbslq_u8(vcltq_u8(lo, vdupq_n_u8(26)), vaddq_u8(lo, vdupq_n_u8(26)), r);
  uint8x16_t dg = vsubq_u8(c, vdupq_n_u8('0'));
  r = vbslq_u8(vcltq_u8(dg, vdupq_n_u8(10)), vaddq_u8(dg, vdupq_n_u8(52)), r);
  r = vbslq_u8(vceqq_u8(c, vdupq_n_u8(url ? '-' : '+')), vdupq_n_u8(62), r);
  r = vbslq_u8(vceqq_u8(c, vdupq_n_u8(url ? '_' : '/')), vdupq_n_u8(63), r);
  return r;
}
#endif

// Encode the complete 3-byte groups of src[0:n] (n/3*3 bytes) into dst
// (4 characters per group, no padding; the caller encodes the tail).
// Returns the number of source bytes consumed. url selects - and _.
RT_API intptr_t rt_base64_encode(uint8_t *dst, const void *src, intptr_t n, int32_t url) {
  bp s = src;
  intptr_t i = 0, o = 0;
#if RT_NEON
  for (; i + 48 <= n; i += 48, o += 64) {
    uint8x16x3_t in = vld3q_u8(s + i);
    uint8x16_t a = in.val[0], b = in.val[1], c = in.val[2];
    uint8x16x4_t out;
    out.val[0] = b64_enc_vec(vshrq_n_u8(a, 2), url);
    out.val[1] = b64_enc_vec(vorrq_u8(vshlq_n_u8(vandq_u8(a, vdupq_n_u8(3)), 4), vshrq_n_u8(b, 4)), url);
    out.val[2] = b64_enc_vec(vorrq_u8(vshlq_n_u8(vandq_u8(b, vdupq_n_u8(15)), 2), vshrq_n_u8(c, 6)), url);
    out.val[3] = b64_enc_vec(vandq_u8(c, vdupq_n_u8(63)), url);
    vst4q_u8(dst + o, out);
  }
#endif
  for (; i + 3 <= n; i += 3, o += 4) {
    uint32_t v = (uint32_t)s[i] << 16 | (uint32_t)s[i + 1] << 8 | s[i + 2];
    dst[o] = b64_char((uint8_t)(v >> 18), url);
    dst[o + 1] = b64_char((uint8_t)((v >> 12) & 63), url);
    dst[o + 2] = b64_char((uint8_t)((v >> 6) & 63), url);
    dst[o + 3] = b64_char((uint8_t)(v & 63), url);
  }
  return i;
}

// Decode complete 4-character groups of src[0:n] into dst (3 bytes each),
// stopping before the first group that holds a character outside the
// alphabet (padding, newline, error). Returns the number of source
// characters consumed (a multiple of 4); the caller decodes the rest.
RT_API intptr_t rt_base64_decode(uint8_t *dst, const void *src, intptr_t n, int32_t url) {
  bp s = src;
  intptr_t i = 0, o = 0;
#if RT_NEON
  for (; i + 64 <= n; i += 64, o += 48) {
    uint8x16x4_t in = vld4q_u8(s + i);
    uint8x16_t v0 = b64_dec_vec(in.val[0], url), v1 = b64_dec_vec(in.val[1], url);
    uint8x16_t v2 = b64_dec_vec(in.val[2], url), v3 = b64_dec_vec(in.val[3], url);
    if (vmaxvq_u8(vorrq_u8(vorrq_u8(v0, v1), vorrq_u8(v2, v3))) == 0xFF) break;
    uint8x16x3_t out;
    out.val[0] = vorrq_u8(vshlq_n_u8(v0, 2), vshrq_n_u8(v1, 4));
    out.val[1] = vorrq_u8(vshlq_n_u8(v1, 4), vshrq_n_u8(v2, 2));
    out.val[2] = vorrq_u8(vshlq_n_u8(v2, 6), v3);
    vst3q_u8(dst + o, out);
  }
#endif
  for (; i + 4 <= n; i += 4, o += 3) {
    int a = b64_val(s[i], url), b = b64_val(s[i + 1], url), c = b64_val(s[i + 2], url),
        d = b64_val(s[i + 3], url);
    if ((a | b | c | d) < 0) break;
    uint32_t v = (uint32_t)a << 18 | (uint32_t)b << 12 | (uint32_t)c << 6 | (uint32_t)d;
    dst[o] = (uint8_t)(v >> 16);
    dst[o + 1] = (uint8_t)(v >> 8);
    dst[o + 2] = (uint8_t)v;
  }
  return i;
}

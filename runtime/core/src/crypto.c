// crypto.c: SHA-256 and SHA-512 block functions for crypto/sha256 and
// crypto/sha512. ARMv8 SHA2 instructions on arm64, SHA-NI on x86-64 when
// the CPU has it, portable C otherwise. The interface matches the Go
// block functions: state h, then n bytes (a multiple of the block size;
// any remainder is ignored), processed in order.

#include "internal.h"

#if defined(__aarch64__)
#include <arm_neon.h>
#elif defined(__x86_64__)
#include <cpuid.h>
#include <immintrin.h>
#endif

static const uint32_t K256[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
};

static const uint64_t K512[80] = {
    0x428a2f98d728ae22ull, 0x7137449123ef65cdull, 0xb5c0fbcfec4d3b2full, 0xe9b5dba58189dbbcull,
    0x3956c25bf348b538ull, 0x59f111f1b605d019ull, 0x923f82a4af194f9bull, 0xab1c5ed5da6d8118ull,
    0xd807aa98a3030242ull, 0x12835b0145706fbeull, 0x243185be4ee4b28cull, 0x550c7dc3d5ffb4e2ull,
    0x72be5d74f27b896full, 0x80deb1fe3b1696b1ull, 0x9bdc06a725c71235ull, 0xc19bf174cf692694ull,
    0xe49b69c19ef14ad2ull, 0xefbe4786384f25e3ull, 0x0fc19dc68b8cd5b5ull, 0x240ca1cc77ac9c65ull,
    0x2de92c6f592b0275ull, 0x4a7484aa6ea6e483ull, 0x5cb0a9dcbd41fbd4ull, 0x76f988da831153b5ull,
    0x983e5152ee66dfabull, 0xa831c66d2db43210ull, 0xb00327c898fb213full, 0xbf597fc7beef0ee4ull,
    0xc6e00bf33da88fc2ull, 0xd5a79147930aa725ull, 0x06ca6351e003826full, 0x142929670a0e6e70ull,
    0x27b70a8546d22ffcull, 0x2e1b21385c26c926ull, 0x4d2c6dfc5ac42aedull, 0x53380d139d95b3dfull,
    0x650a73548baf63deull, 0x766a0abb3c77b2a8ull, 0x81c2c92e47edaee6ull, 0x92722c851482353bull,
    0xa2bfe8a14cf10364ull, 0xa81a664bbc423001ull, 0xc24b8b70d0f89791ull, 0xc76c51a30654be30ull,
    0xd192e819d6ef5218ull, 0xd69906245565a910ull, 0xf40e35855771202aull, 0x106aa07032bbd1b8ull,
    0x19a4c116b8d2d0c8ull, 0x1e376c085141ab53ull, 0x2748774cdf8eeb99ull, 0x34b0bcb5e19b48a8ull,
    0x391c0cb3c5c95a63ull, 0x4ed8aa4ae3418acbull, 0x5b9cca4f7763e373ull, 0x682e6ff3d6b2b8a3ull,
    0x748f82ee5defb2fcull, 0x78a5636f43172f60ull, 0x84c87814a1f0ab72ull, 0x8cc702081a6439ecull,
    0x90befffa23631e28ull, 0xa4506cebde82bde9ull, 0xbef9a3f7b2c67915ull, 0xc67178f2e372532bull,
    0xca273eceea26619cull, 0xd186b8c721c0c207ull, 0xeada7dd6cde0eb1eull, 0xf57d4f7fee6ed178ull,
    0x06f067aa72176fbaull, 0x0a637dc5a2c898a6ull, 0x113f9804bef90daeull, 0x1b710b35131c471bull,
    0x28db77f523047d84ull, 0x32caab7b40c72493ull, 0x3c9ebe0a15c9bebcull, 0x431d67c49c100d4cull,
    0x4cc5d4becb3e42b6ull, 0x597f299cfc657e2aull, 0x5fcb6fab3ad6faecull, 0x6c44198c4a475817ull,
};

#define ROR32(x, n) (((x) >> (n)) | ((x) << (32 - (n))))
#define ROR64(x, n) (((x) >> (n)) | ((x) << (64 - (n))))

INLINE uint32_t be32(const uint8_t *p) {
  return (uint32_t)p[0] << 24 | (uint32_t)p[1] << 16 | (uint32_t)p[2] << 8 | p[3];
}

INLINE uint64_t be64(const uint8_t *p) { return (uint64_t)be32(p) << 32 | be32(p + 4); }

// ---- portable ----

void sha256_block_generic(uint32_t h[8], const uint8_t *p, intptr_t n) {
  uint32_t w[64];
  for (; n >= 64; n -= 64, p += 64) {
    for (int i = 0; i < 16; i++) w[i] = be32(p + 4 * i);
    for (int i = 16; i < 64; i++) {
      uint32_t s0 = ROR32(w[i - 15], 7) ^ ROR32(w[i - 15], 18) ^ (w[i - 15] >> 3);
      uint32_t s1 = ROR32(w[i - 2], 17) ^ ROR32(w[i - 2], 19) ^ (w[i - 2] >> 10);
      w[i] = w[i - 16] + s0 + w[i - 7] + s1;
    }
    uint32_t a = h[0], b = h[1], c = h[2], d = h[3], e = h[4], f = h[5], g = h[6], hh = h[7];
    for (int i = 0; i < 64; i++) {
      uint32_t t1 = hh + (ROR32(e, 6) ^ ROR32(e, 11) ^ ROR32(e, 25)) + ((e & f) ^ (~e & g)) + K256[i] + w[i];
      uint32_t t2 = (ROR32(a, 2) ^ ROR32(a, 13) ^ ROR32(a, 22)) + ((a & b) ^ (a & c) ^ (b & c));
      hh = g;
      g = f;
      f = e;
      e = d + t1;
      d = c;
      c = b;
      b = a;
      a = t1 + t2;
    }
    h[0] += a;
    h[1] += b;
    h[2] += c;
    h[3] += d;
    h[4] += e;
    h[5] += f;
    h[6] += g;
    h[7] += hh;
  }
}

#define S512_ROUND(a, b, c, d, e, f, g, h, i)                                                  \
  do {                                                                                     \
    uint64_t t1 = h + (ROR64(e, 14) ^ ROR64(e, 18) ^ ROR64(e, 41)) + ((e & f) ^ (~e & g)) + \
                  K512[i] + w[i];                                                          \
    uint64_t t2 = (ROR64(a, 28) ^ ROR64(a, 34) ^ ROR64(a, 39)) + ((a & b) ^ (a & c) ^ (b & c)); \
    d += t1;                                                                               \
    h = t1 + t2;                                                                           \
  } while (0)

void sha512_block_generic(uint64_t hs[8], const uint8_t *p, intptr_t n) {
  uint64_t w[80];
  for (; n >= 128; n -= 128, p += 128) {
    for (int i = 0; i < 16; i++) w[i] = be64(p + 8 * i);
    for (int i = 16; i < 80; i++) {
      uint64_t s0 = ROR64(w[i - 15], 1) ^ ROR64(w[i - 15], 8) ^ (w[i - 15] >> 7);
      uint64_t s1 = ROR64(w[i - 2], 19) ^ ROR64(w[i - 2], 61) ^ (w[i - 2] >> 6);
      w[i] = w[i - 16] + s0 + w[i - 7] + s1;
    }
    uint64_t a = hs[0], b = hs[1], c = hs[2], d = hs[3], e = hs[4], f = hs[5], g = hs[6], h = hs[7];
    for (int i = 0; i < 80; i += 8) {
      S512_ROUND(a, b, c, d, e, f, g, h, i);
      S512_ROUND(h, a, b, c, d, e, f, g, i + 1);
      S512_ROUND(g, h, a, b, c, d, e, f, i + 2);
      S512_ROUND(f, g, h, a, b, c, d, e, i + 3);
      S512_ROUND(e, f, g, h, a, b, c, d, i + 4);
      S512_ROUND(d, e, f, g, h, a, b, c, i + 5);
      S512_ROUND(c, d, e, f, g, h, a, b, i + 6);
      S512_ROUND(b, c, d, e, f, g, h, a, i + 7);
    }
    hs[0] += a;
    hs[1] += b;
    hs[2] += c;
    hs[3] += d;
    hs[4] += e;
    hs[5] += f;
    hs[6] += g;
    hs[7] += h;
  }
}

// ---- ARMv8 SHA2 ----

#if defined(__aarch64__)
__attribute__((target("sha2"))) static void sha256_block_arm(uint32_t h[8], const uint8_t *p,
                                                              intptr_t n) {
  uint32x4_t s0 = vld1q_u32(h), s1 = vld1q_u32(h + 4);
  for (; n >= 64; n -= 64, p += 64) {
    uint32x4_t a0 = s0, a1 = s1;
    uint32x4_t m[4];
#pragma clang loop unroll(full)
    for (int i = 0; i < 4; i++) m[i] = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(p + 16 * i)));
#pragma clang loop unroll(full)
    for (int i = 0; i < 16; i++) {
      uint32x4_t t = vaddq_u32(m[i & 3], vld1q_u32(K256 + 4 * i));
      if (i < 12)
        m[i & 3] = vsha256su1q_u32(vsha256su0q_u32(m[i & 3], m[(i + 1) & 3]), m[(i + 2) & 3],
                                   m[(i + 3) & 3]);
      uint32x4_t prev = s0;
      s0 = vsha256hq_u32(s0, s1, t);
      s1 = vsha256h2q_u32(s1, prev, t);
    }
    s0 = vaddq_u32(s0, a0);
    s1 = vaddq_u32(s1, a1);
  }
  vst1q_u32(h, s0);
  vst1q_u32(h + 4, s1);
}

// ARMv8.2 SHA512: 40 double rounds over five rotating state registers, as
// in the Linux kernel's sha512-ce-core.S.
__attribute__((target("sha3"))) static void sha512_block_arm(uint64_t h[8], const uint8_t *p,
                                                             intptr_t n) {
  static const uint8_t rot[5][5] = {{0, 1, 2, 3, 4}, {3, 0, 4, 2, 1}, {2, 3, 1, 4, 0},
                                    {4, 2, 0, 1, 3}, {1, 4, 3, 0, 2}};
  uint64x2_t st0 = vld1q_u64(h), st1 = vld1q_u64(h + 2), st2 = vld1q_u64(h + 4),
             st3 = vld1q_u64(h + 6);
  for (; n >= 128; n -= 128, p += 128) {
    uint64x2_t s[5] = {st0, st1, st2, st3, st3};
    uint64x2_t m[8];
#pragma clang loop unroll(full)
    for (int i = 0; i < 8; i++) m[i] = vreinterpretq_u64_u8(vrev64q_u8(vld1q_u8(p + 16 * i)));
#pragma clang loop unroll(full)
    for (int k = 0; k < 40; k++) {
      const int i0 = rot[k % 5][0], i1 = rot[k % 5][1], i2 = rot[k % 5][2], i3 = rot[k % 5][3],
                i4 = rot[k % 5][4];
      uint64x2_t t5 = vaddq_u64(vld1q_u64(K512 + 2 * k), m[k & 7]);
      uint64x2_t t6 = vextq_u64(s[i2], s[i3], 1);
      t5 = vextq_u64(t5, t5, 1);
      uint64x2_t t7 = vextq_u64(s[i1], s[i2], 1);
      s[i3] = vaddq_u64(s[i3], t5);
      uint64x2_t t8 = vdupq_n_u64(0);
      if (k < 32) {
        t8 = vextq_u64(m[(k + 4) & 7], m[(k + 5) & 7], 1);
        m[k & 7] = vsha512su0q_u64(m[k & 7], m[(k + 1) & 7]);
      }
      s[i3] = vsha512hq_u64(s[i3], t6, t7);
      if (k < 32) m[k & 7] = vsha512su1q_u64(m[k & 7], m[(k + 7) & 7], t8);
      s[i4] = vaddq_u64(s[i1], s[i3]);
      s[i3] = vsha512h2q_u64(s[i3], s[i1], s[i0]);
    }
    st0 = vaddq_u64(st0, s[0]);
    st1 = vaddq_u64(st1, s[1]);
    st2 = vaddq_u64(st2, s[2]);
    st3 = vaddq_u64(st3, s[3]);
  }
  vst1q_u64(h, st0);
  vst1q_u64(h + 2, st1);
  vst1q_u64(h + 4, st2);
  vst1q_u64(h + 6, st3);
}
#endif

// ---- x86 SHA-NI ----

#if defined(__x86_64__)
static int shani_state;  // 0 unknown, 1 yes, 2 no

static bool have_shani(void) {
  int s = __atomic_load_n(&shani_state, __ATOMIC_RELAXED);
  if (!s) {
    unsigned a, b, c, d;
    bool yes = __get_cpuid_count(7, 0, &a, &b, &c, &d) && (b & (1u << 29));
    if (yes && __get_cpuid(1, &a, &b, &c, &d)) yes = (c & (1u << 19)) && (c & (1u << 9));
    s = yes ? 1 : 2;
    __atomic_store_n(&shani_state, s, __ATOMIC_RELAXED);
  }
  return s == 1;
}

// Intel's reference schedule for SHA-NI.
__attribute__((target("sha,sse4.1,ssse3"))) static void sha256_block_shani(uint32_t h[8],
                                                                        const uint8_t *p,
                                                                        intptr_t n) {
  const __m128i mask = _mm_set_epi64x(0x0c0d0e0f08090a0bull, 0x0405060700010203ull);
  __m128i tmp = _mm_loadu_si128((const __m128i *)h);
  __m128i st1 = _mm_loadu_si128((const __m128i *)(h + 4));
  tmp = _mm_shuffle_epi32(tmp, 0xB1);          // CDAB
  st1 = _mm_shuffle_epi32(st1, 0x1B);          // EFGH
  __m128i st0 = _mm_alignr_epi8(tmp, st1, 8);  // ABEF
  st1 = _mm_blend_epi16(st1, tmp, 0xF0);       // CDGH
  for (; n >= 64; n -= 64, p += 64) {
    __m128i a0 = st0, a1 = st1;
    __m128i m[4];
#pragma clang loop unroll(full)
    for (int i = 0; i < 4; i++)
      m[i] = _mm_shuffle_epi8(_mm_loadu_si128((const __m128i *)(p + 16 * i)), mask);
#pragma clang loop unroll(full)
    for (int i = 0; i < 16; i++) {
      __m128i msg = _mm_add_epi32(m[i & 3], _mm_loadu_si128((const __m128i *)(K256 + 4 * i)));
      st1 = _mm_sha256rnds2_epu32(st1, st0, msg);
      if (i >= 3 && i < 15) {
        // m[(i+1)&3] for 4 rounds later: finish its schedule.
        __m128i t = _mm_alignr_epi8(m[i & 3], m[(i + 3) & 3], 4);
        m[(i + 1) & 3] = _mm_sha256msg2_epu32(_mm_add_epi32(m[(i + 1) & 3], t), m[i & 3]);
      }
      msg = _mm_shuffle_epi32(msg, 0x0E);
      st0 = _mm_sha256rnds2_epu32(st0, st1, msg);
      if (i >= 1 && i < 13) m[(i + 3) & 3] = _mm_sha256msg1_epu32(m[(i + 3) & 3], m[i & 3]);
    }
    st0 = _mm_add_epi32(st0, a0);
    st1 = _mm_add_epi32(st1, a1);
  }
  tmp = _mm_shuffle_epi32(st0, 0x1B);          // FEBA
  st1 = _mm_shuffle_epi32(st1, 0xB1);          // DCHG
  st0 = _mm_blend_epi16(tmp, st1, 0xF0);       // DCBA
  st1 = _mm_alignr_epi8(st1, tmp, 8);          // ABEF -> HGFE
  _mm_storeu_si128((__m128i *)h, st0);
  _mm_storeu_si128((__m128i *)(h + 4), st1);
}
#endif

RT_API void rt_sha256_block(uint32_t h[8], const uint8_t *p, intptr_t n) {
#if defined(__aarch64__)
  sha256_block_arm(h, p, n);
#elif defined(__x86_64__)
  if (have_shani())
    sha256_block_shani(h, p, n);
  else
    sha256_block_generic(h, p, n);
#else
  sha256_block_generic(h, p, n);
#endif
}

RT_API void rt_sha512_block(uint64_t h[8], const uint8_t *p, intptr_t n) {
#if defined(__aarch64__)
  sha512_block_arm(h, p, n);  // every Apple silicon and ARMv8.2+ server core
#else
  sha512_block_generic(h, p, n);
#endif
}

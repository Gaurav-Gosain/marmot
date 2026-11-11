// AES (FIPS-197 vectors), GCM (McGrew-Viega test cases 2 and 3), and the
// accelerated paths against the portable ones.
// expect-stdout: ok
#include "rtest.h"

void aes_enc_generic(int nr, const uint8_t *rk, uint8_t *out, const uint8_t *in);
void aes_dec_generic(int nr, const uint8_t *dk, uint8_t *out, const uint8_t *in);
void ghash_mul_generic(uint8_t x[16], const uint8_t h[16]);

static int unhex(const char *s, uint8_t *out) {
  int n = 0;
  for (; s[0] && s[1]; s += 2) {
    int hi = s[0] <= '9' ? s[0] - '0' : s[0] - 'a' + 10, lo = s[1] <= '9' ? s[1] - '0' : s[1] - 'a' + 10;
    out[n++] = (uint8_t)(hi << 4 | lo);
  }
  return n;
}

static void gcm_seal(const uint8_t *key, int kl, const uint8_t iv[12], const uint8_t *pt, int n,
                     uint8_t *ct, uint8_t tag[16]) {
  uint8_t rk[240], h[16] = {0}, j0[16] = {0}, ctr[16], y[16] = {0};
  int nr = rt_aes_expand(key, kl, rk, 0);
  rt_aes_encrypt_blocks(nr, rk, h, h, 1);
  memcpy(j0, iv, 12);
  j0[15] = 1;
  memcpy(ctr, j0, 16);
  ctr[15] = 2;
  rt_aes_ctr32(nr, rk, ctr, ct, pt, n);
  uint8_t buf[256] = {0};
  int padded = (n + 15) / 16 * 16;
  memcpy(buf, ct, (size_t)n);
  rt_ghash(h, y, buf, padded);
  uint8_t lens[16] = {0};
  uint64_t bits = (uint64_t)n * 8;
  for (int i = 0; i < 8; i++) lens[15 - i] = (uint8_t)(bits >> (8 * i));
  rt_ghash(h, y, lens, 16);
  uint8_t ek[16];
  rt_aes_encrypt_blocks(nr, rk, ek, j0, 1);
  for (int i = 0; i < 16; i++) tag[i] = ek[i] ^ y[i];
}

void marmot_main(void) {
  uint8_t key[32], pt[64], want[64], got[64], rk[240], dk[240];
  unhex("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", key);
  unhex("00112233445566778899aabbccddeeff", pt);
  const char *wants[3] = {"69c4e0d86a7b0430d8cdb78070b4c55a", "dda97ca4864cdfe06eaf70a0ec0d7191",
                          "8ea2b7ca516745bfeafc49904b496089"};
  int kls[3] = {16, 24, 32};
  for (int k = 0; k < 3; k++) {
    int nr = rt_aes_expand(key, kls[k], rk, dk);
    CHECK(nr == 10 + 2 * k);
    unhex(wants[k], want);
    rt_aes_encrypt_blocks(nr, rk, got, pt, 1);
    CHECK(memcmp(got, want, 16) == 0);
    rt_aes_decrypt_blocks(nr, dk, got, got, 1);
    CHECK(memcmp(got, pt, 16) == 0);
    aes_enc_generic(nr, rk, got, pt);
    CHECK(memcmp(got, want, 16) == 0);
    aes_dec_generic(nr, dk, got, want);
    CHECK(memcmp(got, pt, 16) == 0);
  }
  // GCM test case 2.
  uint8_t zero[16] = {0}, tag[16], ct[64];
  gcm_seal(zero, 16, zero, zero, 16, ct, tag);
  unhex("0388dace60b6a392f328c2b971b2fe78", want);
  CHECK(memcmp(ct, want, 16) == 0);
  unhex("ab6e47d42cec13bdf53a67b21257bddf", want);
  CHECK(memcmp(tag, want, 16) == 0);
  // GCM test case 3.
  uint8_t k3[16], iv[12], p3[64];
  unhex("feffe9928665731c6d6a8f9467308308", k3);
  unhex("cafebabefacedbaddecaf888", iv);
  unhex("d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532fcf0e2449a6b525b16aedf5aa0de657ba637b391aafd255", p3);
  gcm_seal(k3, 16, iv, p3, 64, ct, tag);
  unhex("42831ec2217774244b7221b784d0d49ce3aa212f2c02a4e035c17e2329aca12e21d514b25466931c7d8f6a5aac84aa051ba30b396a0aac973d58e091473f5985", want);
  CHECK(memcmp(ct, want, 64) == 0);
  unhex("4d5c2af327cd64a62cf35abd2ba6fab4", want);
  CHECK(memcmp(tag, want, 16) == 0);
  // Random: accelerated vs portable.
  uint64_t rs = 7;
  for (int iter = 0; iter < 2000; iter++) {
    uint8_t h[16], y1[16], y2[16], data[64];
    for (int i = 0; i < 16; i++) { rs ^= rs << 13; rs ^= rs >> 7; rs ^= rs << 17; h[i] = (uint8_t)rs; y1[i] = y2[i] = (uint8_t)(rs >> 8); }
    for (int i = 0; i < 64; i++) { rs ^= rs << 13; rs ^= rs >> 7; rs ^= rs << 17; data[i] = (uint8_t)rs; }
    rt_ghash(h, y1, data, 64);
    for (int b = 0; b < 4; b++) { for (int i = 0; i < 16; i++) y2[i] ^= data[16 * b + i]; ghash_mul_generic(y2, h); }
    CHECK(memcmp(y1, y2, 16) == 0);
    int nr = rt_aes_expand(data, kls[iter % 3], rk, dk);
    uint8_t a[16], b2[16];
    rt_aes_encrypt_blocks(nr, rk, a, data + 32, 1);
    aes_enc_generic(nr, rk, b2, data + 32);
    CHECK(memcmp(a, b2, 16) == 0);
  }
  out("ok\n");
}

// SHA-256 and SHA-512 block functions: known vectors and the portable code.
// expect-stdout: ok
#include "rtest.h"

void sha256_block_generic(uint32_t h[8], const uint8_t *p, intptr_t n);
void sha512_block_generic(uint64_t h[8], const uint8_t *p, intptr_t n);

static void sha256(const uint8_t *msg, size_t len, uint8_t out[32], int generic) {
  uint32_t h[8] = {0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19};
  uint8_t buf[256];
  size_t full = len / 64 * 64;
  if (generic) sha256_block_generic(h, msg, (intptr_t)full); else rt_sha256_block(h, msg, (intptr_t)full);
  size_t rem = len - full;
  memcpy(buf, msg + full, rem);
  buf[rem] = 0x80;
  size_t pad = rem < 56 ? 64 : 128;
  memset(buf + rem + 1, 0, pad - rem - 1);
  uint64_t bits = (uint64_t)len * 8;
  for (int i = 0; i < 8; i++) buf[pad - 1 - i] = (uint8_t)(bits >> (8 * i));
  if (generic) sha256_block_generic(h, buf, (intptr_t)pad); else rt_sha256_block(h, buf, (intptr_t)pad);
  for (int i = 0; i < 8; i++) for (int k = 0; k < 4; k++) out[4 * i + k] = (uint8_t)(h[i] >> (24 - 8 * k));
}

static void sha512(const uint8_t *msg, size_t len, uint8_t out[64]) {
  uint64_t h[8] = {0x6a09e667f3bcc908ull, 0xbb67ae8584caa73bull, 0x3c6ef372fe94f82bull, 0xa54ff53a5f1d36f1ull,
                   0x510e527fade682d1ull, 0x9b05688c2b3e6c1full, 0x1f83d9abfb41bd6bull, 0x5be0cd19137e2179ull};
  uint8_t buf[256];
  size_t full = len / 128 * 128;
  rt_sha512_block(h, msg, (intptr_t)full);
  size_t rem = len - full;
  memcpy(buf, msg + full, rem);
  buf[rem] = 0x80;
  size_t pad = rem < 112 ? 128 : 256;
  memset(buf + rem + 1, 0, pad - rem - 1);
  uint64_t bits = (uint64_t)len * 8;
  for (int i = 0; i < 8; i++) buf[pad - 1 - i] = (uint8_t)(bits >> (8 * i));
  rt_sha512_block(h, buf, (intptr_t)pad);
  for (int i = 0; i < 8; i++) for (int k = 0; k < 8; k++) out[8 * i + k] = (uint8_t)(h[i] >> (56 - 8 * k));
}

static void hex(const uint8_t *b, int n, char *o) {
  for (int i = 0; i < n; i++) { o[2 * i] = "0123456789abcdef"[b[i] >> 4]; o[2 * i + 1] = "0123456789abcdef"[b[i] & 15]; }
  o[2 * n] = 0;
}

void marmot_main(void) {
  uint8_t d[64];
  char hx[129];
  sha256((const uint8_t *)"abc", 3, d, 0);
  hex(d, 32, hx);
  CHECK(strcmp(hx, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") == 0);
  sha256((const uint8_t *)"", 0, d, 0);
  hex(d, 32, hx);
  CHECK(strcmp(hx, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855") == 0);
  const char *m2 = "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq";
  sha256((const uint8_t *)m2, strlen(m2), d, 0);
  hex(d, 32, hx);
  CHECK(strcmp(hx, "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1") == 0);
  sha512((const uint8_t *)"abc", 3, d);
  hex(d, 64, hx);
  CHECK(strcmp(hx, "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f") == 0);
  // Random lengths: accelerated vs portable.
  static uint8_t big[10000];
  uint64_t rs = 99;
  for (int i = 0; i < 10000; i++) { rs ^= rs << 13; rs ^= rs >> 7; rs ^= rs << 17; big[i] = (uint8_t)rs; }
  for (size_t len = 0; len < 10000; len += 1 + len / 7) {
    uint8_t a[32], b[32];
    sha256(big, len, a, 0);
    sha256(big, len, b, 1);
    CHECK(memcmp(a, b, 32) == 0);
  }
  for (intptr_t nb = 0; nb < 70; nb++) {
    uint64_t x[8], y[8];
    for (int i = 0; i < 8; i++) x[i] = y[i] = 0x0123456789abcdefull * (uint64_t)(i + 1);
    rt_sha512_block(x, big + nb, nb * 128);
    sha512_block_generic(y, big + nb, nb * 128);
    CHECK(memcmp(x, y, 64) == 0);
  }
  out("ok\n");
}

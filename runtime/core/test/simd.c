// SIMD byte primitives against naive versions, all lengths and offsets, and
// at a page boundary so over-reads would fault.
// expect-stdout: ok
#include "rtest.h"
#include <sys/mman.h>
#include <unistd.h>

static intptr_t n_index_byte(const uint8_t *p, intptr_t n, uint8_t c) {
  for (intptr_t i = 0; i < n; i++) if (p[i] == c) return i;
  return -1;
}
static intptr_t n_last(const uint8_t *p, intptr_t n, uint8_t c) {
  for (intptr_t i = n - 1; i >= 0; i--) if (p[i] == c) return i;
  return -1;
}
static intptr_t n_count(const uint8_t *p, intptr_t n, uint8_t c) {
  intptr_t k = 0;
  for (intptr_t i = 0; i < n; i++) k += p[i] == c;
  return k;
}
static intptr_t n_index(const uint8_t *p, intptr_t n, const uint8_t *q, intptr_t m) {
  for (intptr_t i = 0; i + m <= n; i++) if (memcmp(p + i, q, (size_t)m) == 0) return i;
  return -1;
}
// Go's utf8 rules, written plainly.
static intptr_t dec(const uint8_t *p, intptr_t n) {  // returns length, 0 invalid
  uint8_t b = p[0];
  if (b < 0x80) return 1;
  int len; uint32_t r;
  if (b >= 0xC0 && b < 0xE0) { len = 2; r = b & 0x1F; }
  else if (b >= 0xE0 && b < 0xF0) { len = 3; r = b & 0x0F; }
  else if (b >= 0xF0 && b < 0xF8) { len = 4; r = b & 0x07; }
  else return 0;
  if (n < len) return 0;
  for (int k = 1; k < len; k++) { if ((p[k] & 0xC0) != 0x80) return 0; r = r << 6 | (p[k] & 0x3F); }
  if ((len == 2 && r < 0x80) || (len == 3 && r < 0x800) || (len == 4 && r < 0x10000)) return 0;
  if (r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF)) return 0;
  return len;
}
static int n_valid(const uint8_t *p, intptr_t n) {
  for (intptr_t i = 0; i < n;) { intptr_t k = dec(p + i, n - i); if (!k) return 0; i += k; }
  return 1;
}
static intptr_t n_runes(const uint8_t *p, intptr_t n) {
  intptr_t r = 0;
  for (intptr_t i = 0; i < n; r++) { intptr_t k = dec(p + i, n - i); i += k ? k : 1; }
  return r;
}

static uint64_t rs = 88172645463325252ull;
static uint64_t rnd(void) { rs ^= rs << 13; rs ^= rs >> 7; rs ^= rs << 17; return rs; }

void marmot_main(void) {
  long ps = sysconf(_SC_PAGESIZE);
  uint8_t *map = mmap(0, (size_t)ps * 2, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANON, -1, 0);
  CHECK(map != MAP_FAILED);
  CHECK(mprotect(map + ps, (size_t)ps, PROT_NONE) == 0);
  for (int iter = 0; iter < 300; iter++) {
    for (intptr_t n = 0; n <= 200; n++) {
      uint8_t *p = map + ps - n;  // ends exactly at the guard page
      int alpha = iter % 3 == 0 ? 4 : 256;
      for (intptr_t i = 0; i < n; i++) p[i] = (uint8_t)(rnd() % (uint64_t)alpha);
      uint8_t c = (uint8_t)(rnd() % (uint64_t)alpha);
      CHECK(rt_index_byte(p, n, c) == n_index_byte(p, n, c));
      CHECK(rt_last_index_byte(p, n, c) == n_last(p, n, c));
      CHECK(rt_count_byte(p, n, c) == n_count(p, n, c));
      intptr_t m = n ? (intptr_t)(rnd() % (uint64_t)(n < 12 ? n + 1 : 12)) : 0;
      intptr_t at = n - m ? (intptr_t)(rnd() % (uint64_t)(n - m + 1)) : 0;
      uint8_t sep[16];
      memcpy(sep, p + at, (size_t)m);
      if (iter % 2) sep[m ? m - 1 : 0] ^= 1;
      CHECK(rt_index(p, n, sep, m) == n_index(p, n, sep, m));
      uint8_t *q = map + ps - 2 * n - 1 > map ? map : map;  // copy for equality
      (void)q;
      uint8_t cp[256];
      memcpy(cp, p, (size_t)n);
      CHECK(rt_mem_equal(p, cp, n) == 1);
      if (n) {
        intptr_t k = (intptr_t)(rnd() % (uint64_t)n);
        cp[k] ^= 0x10;
        CHECK(rt_mem_equal(p, cp, n) == 0);
        int want = memcmp(p, cp, (size_t)n) < 0 ? -1 : 1;
        CHECK(rt_compare(p, n, cp, n) == want);
        CHECK(rt_compare(p, n - 1, p, n) == -1);
      }
      // UTF-8: mix of ASCII and multibyte, sometimes broken.
      for (intptr_t i = 0; i < n; i++) {
        uint64_t r = rnd() % 10;
        p[i] = r < 6 ? (uint8_t)(rnd() % 128) : (uint8_t)(0x80 + rnd() % 128);
      }
      CHECK(rt_utf8_valid(p, n) == n_valid(p, n));
      CHECK(rt_utf8_count(p, n) == n_runes(p, n));
      CHECK(rt_ascii_prefix(p, n) == ({ intptr_t a = 0; while (a < n && p[a] < 0x80) a++; a; }));
      (void)rt_hash_bytes(p, n, 1);
    }
  }
  // Random valid UTF-8 (all sequence lengths), then single corruptions.
  for (int iter = 0; iter < 20000; iter++) {
    uint8_t b[400];
    intptr_t n = 0, target = (intptr_t)(rnd() % 380);
    while (n < target) {
      uint32_t r;
      switch (rnd() % 4) {
        case 0: r = (uint32_t)(rnd() % 0x80); break;
        case 1: r = 0x80 + (uint32_t)(rnd() % (0x800 - 0x80)); break;
        case 2: do r = 0x800 + (uint32_t)(rnd() % (0x10000 - 0x800)); while (r >= 0xD800 && r <= 0xDFFF); break;
        default: r = 0x10000 + (uint32_t)(rnd() % (0x110000 - 0x10000)); break;
      }
      if (r < 0x80) b[n++] = (uint8_t)r;
      else if (r < 0x800) { b[n++] = (uint8_t)(0xC0 | r >> 6); b[n++] = (uint8_t)(0x80 | (r & 0x3F)); }
      else if (r < 0x10000) { b[n++] = (uint8_t)(0xE0 | r >> 12); b[n++] = (uint8_t)(0x80 | ((r >> 6) & 0x3F)); b[n++] = (uint8_t)(0x80 | (r & 0x3F)); }
      else { b[n++] = (uint8_t)(0xF0 | r >> 18); b[n++] = (uint8_t)(0x80 | ((r >> 12) & 0x3F)); b[n++] = (uint8_t)(0x80 | ((r >> 6) & 0x3F)); b[n++] = (uint8_t)(0x80 | (r & 0x3F)); }
    }
    CHECK(rt_utf8_valid(b, n) == 1);
    CHECK(rt_utf8_count(b, n) == n_runes(b, n));
    if (n > 0 && iter % 2) {
      b[rnd() % (uint64_t)n] = (uint8_t)rnd();
      CHECK(rt_utf8_valid(b, n) == n_valid(b, n));
      CHECK(rt_utf8_count(b, n) == n_runes(b, n));
      // Truncation.
      intptr_t t = (intptr_t)(rnd() % (uint64_t)n);
      CHECK(rt_utf8_valid(b, t) == n_valid(b, t));
    }
  }
  // Digits and hex.
  for (int iter = 0; iter < 20000; iter++) {
    uint8_t d[40];
    intptr_t n = (intptr_t)(rnd() % 30);
    for (intptr_t i = 0; i < n; i++) d[i] = (uint8_t)('0' + rnd() % 10);
    intptr_t stop = n ? (intptr_t)(rnd() % (uint64_t)(n + 1)) : 0;
    if (stop < n && iter % 3) d[stop] = (uint8_t)"x.-/:e "[rnd() % 7];
    uint64_t want = 0;
    intptr_t k = 0;
    while (k < n && k < 19 && d[k] >= '0' && d[k] <= '9') want = want * 10 + (uint64_t)(d[k++] - '0');
    uint64_t got = 12345;
    CHECK(rt_parse_digits(d, n, &got) == k && got == want);
    uint8_t raw[40], hx[80], back[40];
    for (intptr_t i = 0; i < n; i++) raw[i] = (uint8_t)rnd();
    rt_hex_encode(hx, raw, n);
    for (intptr_t i = 0; i < n; i++) CHECK(hx[2 * i] == "0123456789abcdef"[raw[i] >> 4] && hx[2 * i + 1] == "0123456789abcdef"[raw[i] & 15]);
    if (iter % 2) for (intptr_t i = 0; i < 2 * n; i++) if (hx[i] >= 'a' && rnd() % 2) hx[i] -= 32;  // uppercase ok
    CHECK(rt_hex_decode(back, hx, n) == n && memcmp(back, raw, (size_t)n) == 0);
    if (n) {
      intptr_t bad = (intptr_t)(rnd() % (uint64_t)(2 * n));
      hx[bad] = 'g';
      CHECK(rt_hex_decode(back, hx, n) == -(bad + 1));
    }
  }
  // base64 against a scalar reference.
  for (int iter = 0; iter < 5000; iter++) {
    int url = iter & 1;
    const char *alpha = url ? "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
                            : "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    uint8_t raw[300], enc[400], dec[300];
    intptr_t n = (intptr_t)(rnd() % 300);
    for (intptr_t i = 0; i < n; i++) raw[i] = (uint8_t)rnd();
    intptr_t used = rt_base64_encode(enc, raw, n, url);
    CHECK(used == n / 3 * 3);
    for (intptr_t g = 0; g < used / 3; g++) {
      uint32_t v = (uint32_t)raw[3 * g] << 16 | (uint32_t)raw[3 * g + 1] << 8 | raw[3 * g + 2];
      CHECK(enc[4 * g] == alpha[v >> 18] && enc[4 * g + 1] == alpha[(v >> 12) & 63] &&
            enc[4 * g + 2] == alpha[(v >> 6) & 63] && enc[4 * g + 3] == alpha[v & 63]);
    }
    intptr_t chars = used / 3 * 4;
    intptr_t bad = -1;
    if (chars && iter % 3 == 0) { bad = (intptr_t)(rnd() % (uint64_t)chars); enc[bad] = (uint8_t)"=\n*. "[rnd() % 5]; }
    intptr_t got = rt_base64_decode(dec, enc, chars, url);
    intptr_t want = bad < 0 ? chars : bad / 4 * 4;
    CHECK(got == want);
    CHECK(memcmp(dec, raw, (size_t)(got / 4 * 3)) == 0);
  }
  // Valid multibyte text.
  const char *txt = "h\xc3\xa9llo w\xc3\xb6rld \xe4\xb8\x96\xe7\x95\x8c \xf0\x9f\x98\x80 end";
  intptr_t tn = (intptr_t)strlen(txt);
  CHECK(rt_utf8_valid(txt, tn) == 1 && rt_utf8_count(txt, tn) == n_runes((const uint8_t *)txt, tn));
  CHECK(rt_utf8_valid("\xed\xa0\x80", 3) == 0);  // surrogate
  CHECK(rt_utf8_valid("\xc0\x80", 2) == 0);      // overlong
  CHECK(rt_utf8_valid("\xf4\x90\x80\x80", 4) == 0);  // > U+10FFFF
  // Hash: deterministic per seed, sensitive to every byte and the length.
  uint8_t h[64] = {0};
  uint64_t h0 = rt_hash_bytes(h, 64, 7);
  CHECK(h0 == rt_hash_bytes(h, 64, 7) && h0 != rt_hash_bytes(h, 64, 8) && h0 != rt_hash_bytes(h, 63, 7));
  for (int i = 0; i < 64; i++) { h[i] = 1; CHECK(rt_hash_bytes(h, 64, 7) != h0); h[i] = 0; }
  CHECK(rt_hash_u64(1, 0) != rt_hash_u64(2, 0));
  out("ok\n");
}

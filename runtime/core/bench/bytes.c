// Byte primitives throughput. Go counterpart: go/bytes_test.go style program go/bytes.go.
#include "rt.h"
#include <stdio.h>
#include <string.h>

static volatile intptr_t sink;

#define BENCH(name, bytes, reps, expr)                                                     \
  do {                                                                                     \
    int64_t t0 = rt_nanotime();                                                            \
    for (long r = 0; r < (reps); r++) sink += (intptr_t)(expr);                            \
    double s = (rt_nanotime() - t0) / 1e9;                                                 \
    printf("%-34s %8.2f GB/s  %8.1f ns/op\n", name, (double)(bytes) * (reps) / s / 1e9, s * 1e9 / (reps)); \
    fflush(stdout);                                                                        \
  } while (0)

void marmot_main(void) {
  long n = 1 << 20;
  char *buf = rt_alloc((uintptr_t)n, RT_ALLOC_NOSCAN, 0, 0);
  for (long i = 0; i < n; i++) buf[i] = (char)('a' + i % 23);
  char *ub = rt_alloc((uintptr_t)n, RT_ALLOC_NOSCAN, 0, 0);
  for (long i = 0; i < n; i++) ub[i] = i % 7 ? (char)('a' + i % 23) : 0;
  const char *u8 = "\xc3\xa9";
  for (long i = 0; i + 2 <= n; i += 2) memcpy(ub + i, i % 14 ? "ab" : u8, 2);
  BENCH("Count 1MiB", n, 2000, rt_count_byte(buf, n, 'q'));
  BENCH("IndexByte 1MiB (absent)", n, 2000, rt_index_byte(buf, n, 'Z'));
  BENCH("Index 1MiB 8-byte sep (absent)", n, 2000, rt_index(buf, n, "abcdefgZ", 8));
  BENCH("utf8.Valid 1MiB ASCII", n, 2000, rt_utf8_valid(buf, n));
  BENCH("utf8.Valid 1MiB mixed", n, 500, rt_utf8_valid(ub, n));
  BENCH("RuneCount 1MiB ASCII", n, 2000, rt_utf8_count(buf, n));
  BENCH("hash 1MiB", n, 2000, rt_hash_bytes(buf, n, 1));
  BENCH("hash 16B", 16, 50000000, rt_hash_bytes(buf + (sink & 7), 16, 1));
  BENCH("Equal 1MiB", n, 2000, rt_mem_equal(buf, buf + 0, n));
  BENCH("Index 64B in short string", 64, 20000000, rt_index(buf + (sink & 1), 64, "uvw", 3));
  const char *num = "1234567890123456789";
  uint64_t pv;
  BENCH("parse 19 digits", 19, 50000000, (rt_parse_digits(num + (sink & 0), 19, &pv), pv));
  BENCH("parse 6 digits", 6, 50000000, (rt_parse_digits(num + 13 + (sink & 0), 6, &pv), pv));
  uint8_t *hx = rt_alloc((uintptr_t)n * 2, RT_ALLOC_NOSCAN, 0, 0);
  BENCH("hex encode 1MiB", n, 500, (rt_hex_encode(hx, buf, n), hx[0]));
  BENCH("hex decode 1MiB", n, 500, rt_hex_decode((uint8_t *)ub, hx, n));
  BENCH("crc32 IEEE 1MiB", n, 500, rt_crc32_update(1, (const uint8_t *)buf, n, 0));
  BENCH("crc32 Castagnoli 1MiB", n, 500, rt_crc32_update(1, (const uint8_t *)buf, n, 1));
  uint32_t h256[8] = {0};
  uint64_t h512[8] = {0};
  BENCH("sha256 blocks 1MiB", n, 200, (rt_sha256_block(h256, (const uint8_t *)buf, n), h256[0]));
  BENCH("sha512 blocks 1MiB", n, 200, (rt_sha512_block(h512, (const uint8_t *)buf, n), h512[0]));
  uint8_t rk[240], ctr[16] = {0}, hk[16] = {1}, y[16] = {0};
  int nr = rt_aes_expand((const uint8_t *)"0123456789abcdef", 16, rk, 0);
  char *dst = rt_alloc((uintptr_t)n, RT_ALLOC_NOSCAN, 0, 0);
  BENCH("aes-128-gcm seal 1MiB (ctr+ghash)", n, 200,
        (rt_aes_ctr32(nr, rk, ctr, (uint8_t *)dst, (const uint8_t *)buf, n), rt_ghash(hk, y, (const uint8_t *)dst, n), y[0]));
}

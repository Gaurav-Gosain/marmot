// The collector scans only the data pages a program has touched: a large
// untouched bss array stays untouched (not made resident) across
// collections, and a heap pointer stored on one page in the middle of it
// keeps its object alive.
// expect-stdout: ok
// env: GOMAXPROCS=1
// wasm: skip
#include "rtest.h"
#include <stdio.h>
#include <unistd.h>
#ifdef __linux__
#include <fcntl.h>
#else
#include <sys/mman.h>
#endif

typedef struct cell {
  struct cell *next;
  uintptr_t val;
} cell;
static const uint8_t cell_map[1] = {1};

#define NSLOTS (8u << 20)  // 64 MB of pointer slots in bss
static cell *slots[NSLOTS];
#define MID (NSLOTS / 2 + 7)

// Pages of slots[] that hold data (present or swapped, or touched on darwin).
static uintptr_t touched_pages(void) {
  uintptr_t ps = (uintptr_t)sysconf(_SC_PAGESIZE);
  uintptr_t lo = ((uintptr_t)slots + ps - 1) & ~(ps - 1), hi = ((uintptr_t)(slots + NSLOTS)) & ~(ps - 1);
  uintptr_t n = 0;
#ifdef __linux__
  int fd = open("/proc/self/pagemap", O_RDONLY);
  CHECK(fd >= 0);
  for (uintptr_t a = lo; a < hi; a += ps) {
    uint64_t e = 0;
    CHECK(pread(fd, &e, 8, (off_t)(a / ps * 8)) == 8);
    n += (e >> 62) != 0;
  }
  close(fd);
#else
  static unsigned char vec[(64u << 20) / 4096];  // unsigned: the flags are bits
  CHECK(mincore((void *)lo, hi - lo, (char *)vec) == 0);
  for (uintptr_t i = 0; i < (hi - lo) / ps; i++) n += (vec[i] & (unsigned char)~MINCORE_ANONYMOUS) != 0;
#endif
  return n;
}

void marmot_main(void) {
  cell *c = rt_alloc(sizeof(cell), RT_ALLOC_PRECISE, cell_map, sizeof(cell));
  c->val = 12345;
  slots[MID] = c;  // the only reference
  c = NULL;
  uintptr_t before = touched_pages();
  // One store makes one page resident, or one 2 MB huge page (Linux THP).
  uintptr_t ps = (uintptr_t)sysconf(_SC_PAGESIZE), most = (2u << 20) / ps + 1;
  if (before < 1 || before > most) {
    char b[64];
    int k = snprintf(b, sizeof b, "touched pages before: %lu\n", (unsigned long)before);
    write(2, b, (size_t)k);
  }
  CHECK(before >= 1 && before <= most);
  for (int round = 0; round < 3; round++) {
    // Garbage cells that would reuse the slot of a freed cell.
    for (int i = 0; i < 200000; i++) {
      cell *g = rt_alloc(sizeof(cell), RT_ALLOC_PRECISE, cell_map, sizeof(cell));
      g->val = 1;
    }
    rt_gc_collect();
  }
  CHECK(slots[MID]->val == 12345);
  CHECK(touched_pages() == before);
  out("ok\n");
}

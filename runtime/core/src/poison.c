// poison.c: MARMOT_POISON=1, a use-after-free detector for memory the
// compiler frees early (rt_free, regions).
//
// - rt_free of a small object fills it with 0xFA bytes and quarantines it:
//   it stays allocated (the GC marks it), so it is never handed out again.
//   Every GC checks that the pattern is intact (a write after free is fatal)
//   and a pointer read from freed memory is 0xfafafafafafafafa, which faults
//   when used.
// - rt_free of a large object poisons it and makes its whole OS pages
//   inaccessible: any later access faults.
// - Freed region chunks are poisoned, made inaccessible past the first OS
//   page (the chunk header there stays readable for the GC), and never
//   reused.
// - The fault handler names the freed object or chunk an access hit.
//
// Memory is never returned in this mode; it is for tests, not production.

#include "internal.h"
#include "malloc.h"

#define POISON_BYTE 0xFA

bool rt_poison;

typedef struct qent {
  uintptr_t p, size;
} qent;

static rt_spin qlock;
static qent *qtab;
static uintptr_t qlen, qcap;

static void qadd(uintptr_t p, uintptr_t size) {
  spin_lock(&qlock);
  if (qlen == qcap) {
    uintptr_t ncap = qcap ? qcap * 2 : 4096;
    qent *n = os_map(ncap * sizeof *n);
    if (!n) rt_throw("MARMOT_POISON: out of memory for the quarantine");
    if (qlen) __builtin_memcpy(n, qtab, qlen * sizeof *n);
    if (qtab) os_unmap(qtab, qcap * sizeof *qtab);
    qtab = n;
    qcap = ncap;
  }
  qtab[qlen].p = p;
  qtab[qlen].size = size;
  qlen++;
  spin_unlock(&qlock);
}

void poison_init(void) {
  const char *e = rt_getenv("MARMOT_POISON");
  rt_poison = e && e[0] && e[0] != '0';
}

// Fill [p, p+n) with the pattern and protect the whole OS pages inside
// [lo, p+n) (lo: lowest address that may be protected).
static void poison_range(uintptr_t lo, uintptr_t p, uintptr_t n) {
  __builtin_memset((void *)p, POISON_BYTE, n);
  uintptr_t a = (lo + rt_pagesize - 1) & ~(rt_pagesize - 1);
  uintptr_t b = (p + n) & ~(rt_pagesize - 1);
  if (b > a) os_protect_none((void *)a, b - a);
}

// rt_free in poison mode. s is the object's span; the caller validated p.
void poison_free_small(uintptr_t p, uintptr_t size) {
  __builtin_memset((void *)p, POISON_BYTE, size);
  qadd(p, size);
}

// Large object: heap_lock held by the caller, span still in use.
void poison_free_large(mspan *s) {
  s->kind = RT_ALLOC_NOSCAN;  // never scanned: its pages are inaccessible
  s->poisoned = 1;
  poison_range(s->base, s->base, s->limit - s->base);
  qadd(s->base, s->limit - s->base);
}

// Region chunk data [data, data+n), header before data.
void poison_free_chunk(uintptr_t data, uintptr_t n) {
  // The first OS page holds the chunk header (read by the GC's registry
  // walk): it stays accessible and only poisoned; protection starts at the
  // next page boundary. In this mode region objects start there too
  // (rt_region_alloc_slow), so every freed object is on a protected page.
  poison_range(data, data, n);
}

// World stopped (GC mark): keep every quarantined object marked and check
// that small ones still hold the pattern.
void poison_gc(void (*mark)(uintptr_t p)) {
  for (uintptr_t i = 0; i < qlen; i++) {
    uintptr_t p = qtab[i].p, n = qtab[i].size;
    uintptr_t idx;
    mspan *s = rt_pagemap[(p - rt_arena_start) >> PAGE_SHIFT];
    (void)idx;
    if (!s->poisoned) {
      const uint8_t *b = (const uint8_t *)p;
      for (uintptr_t k = 0; k < n; k++)
        if (b[k] != POISON_BYTE) {
          rt_print("fatal error: MARMOT_POISON: write after free: object ");
          rt_printx(p);
          rt_print(" (");
          rt_printu(n);
          rt_print(" bytes, freed with rt_free) modified at offset ");
          rt_printu(k);
          rt_print("\n");
          os_exit(2);
        }
    }
    mark(p);
  }
}

// Fault handler: true (after printing) if addr is in freed memory.
bool poison_fault(uintptr_t addr, uintptr_t pc) {
  if (!rt_poison) return false;
  for (uintptr_t i = 0; i < qlen; i++)
    if (addr >= qtab[i].p && addr < qtab[i].p + qtab[i].size) {
      rt_print("fatal error: MARMOT_POISON: use after free: access to ");
      rt_printx(addr);
      rt_print(", offset ");
      rt_printu(addr - qtab[i].p);
      rt_print(" in a ");
      rt_printu(qtab[i].size);
      rt_print("-byte object at ");
      rt_printx(qtab[i].p);
      rt_print(" freed with rt_free, pc=");
      rt_printx(pc);
      rt_print("\n");
      return true;
    }
  if (region_poisoned_at(addr)) {
    rt_print("fatal error: MARMOT_POISON: use after free: access to ");
    rt_printx(addr);
    rt_print(" in a freed region chunk, pc=");
    rt_printx(pc);
    rt_print("\n");
    return true;
  }
  if (((uint64_t)addr >> 32) == 0xfafafafa || addr == 0) {
    rt_print("MARMOT_POISON: the fault address ");
    rt_printx(addr);
    rt_print(" may come from a pointer read out of freed memory (pattern 0xfafafafafafafafa; non-canonical addresses fault at 0 on amd64)\n");
  }
  return false;
}

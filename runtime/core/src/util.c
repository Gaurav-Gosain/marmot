// util.c: printing, environment, fast random numbers, metadata allocation.

#include "internal.h"

void rt_printn(const char *s, uintptr_t n) { os_write_err(s, n); }

void rt_print(const char *s) { rt_printn(s, __builtin_strlen(s)); }

void rt_printu(uint64_t v) {
  char buf[24];
  int i = sizeof buf;
  do {
    buf[--i] = (char)('0' + v % 10);
    v /= 10;
  } while (v);
  rt_printn(buf + i, sizeof buf - (uintptr_t)i);
}

void rt_printi(int64_t v) {
  if (v < 0) {
    rt_print("-");
    rt_printu((uint64_t)0 - (uint64_t)v);
  } else {
    rt_printu((uint64_t)v);
  }
}

void rt_printx(uint64_t v) {
  char buf[18];
  int i = sizeof buf;
  do {
    buf[--i] = "0123456789abcdef"[v & 15];
    v >>= 4;
  } while (v);
  buf[--i] = 'x';
  buf[--i] = '0';
  rt_printn(buf + i, sizeof buf - (uintptr_t)i);
}

void rt_throw(const char *msg) {
  rt_print("fatal error: ");
  rt_print(msg);
  rt_print("\n");
  os_exit(2);
}

const char *rt_getenv(const char *name) {
  char **env = rt_envp();
  if (!env) return NULL;
  uintptr_t n = __builtin_strlen(name);
  for (; *env; env++) {
    const char *e = *env;
    if (__builtin_strncmp(e, name, n) == 0 && e[n] == '=') return e + n + 1;
  }
  return NULL;
}

int64_t rt_atoi(const char *s, bool *ok) {
  bool neg = false;
  int64_t v = 0;
  *ok = false;
  if (!s || !*s) return 0;
  if (*s == '-') {
    neg = true;
    s++;
  }
  if (!*s) return 0;
  for (; *s; s++) {
    if (*s < '0' || *s > '9') return 0;
    v = v * 10 + (*s - '0');
  }
  *ok = true;
  return neg ? -v : v;
}

// wyrand on a per-M state, or a global one before Ms exist.
static _Atomic uint64_t global_rand = 0x9E3779B97F4A7C15ull;

uint64_t rt_fastrand(void) {
  rt_m *m = getm();
  uint64_t s;
  if (m) {
    m->fastrand += 0xa0761d6478bd642full;
    s = m->fastrand;
  } else {
    s = atomic_fetch_add_explicit(&global_rand, 0xa0761d6478bd642full, memory_order_relaxed);
  }
  __uint128_t t = (__uint128_t)s * (s ^ 0xe7037ed1a0b428dbull);
  return (uint64_t)(t >> 64) ^ (uint64_t)t;
}

// ---- persistent allocation ----

static rt_spin persist_lock;
static uint8_t *persist_cur, *persist_end;

void *persistent_alloc(uintptr_t size, uintptr_t align) {
  if (align == 0) align = 8;
  if (size >= (64 << 10)) {
    void *p = os_map((size + rt_pagesize - 1) & ~(rt_pagesize - 1));
    if (!p) rt_throw("out of memory (persistent)");
    return p;
  }
  spin_lock(&persist_lock);
  uintptr_t cur = ((uintptr_t)persist_cur + align - 1) & ~(align - 1);
  if (!persist_cur || cur + size > (uintptr_t)persist_end) {
    uintptr_t chunk = 256 << 10;
    persist_cur = os_map(chunk);
    if (!persist_cur) rt_throw("out of memory (persistent)");
    persist_end = persist_cur + chunk;
    cur = (uintptr_t)persist_cur;
  }
  persist_cur = (uint8_t *)(cur + size);
  spin_unlock(&persist_lock);
  return (void *)cur;
}

// ---- fixalloc (caller provides locking) ----

void fixalloc_init(fixalloc *f, uintptr_t size) {
  f->size = (size + 15) & ~(uintptr_t)15;
  f->list = NULL;
  f->chunk = NULL;
  f->nchunk = 0;
  f->inuse = 0;
}

void *fixalloc_alloc(fixalloc *f) {
  void *p;
  if (f->list) {
    p = f->list;
    f->list = *(void **)p;
    __builtin_memset(p, 0, f->size);
  } else {
    if (f->nchunk < f->size) {
      uintptr_t n = 64 << 10;
      if (n < f->size) n = f->size;
      f->chunk = persistent_alloc(n, 16);
      f->nchunk = n;
    }
    p = f->chunk;
    f->chunk += f->size;
    f->nchunk -= f->size;
  }
  f->inuse++;
  return p;
}

void fixalloc_free(fixalloc *f, void *p) {
  *(void **)p = f->list;
  f->list = p;
  f->inuse--;
}

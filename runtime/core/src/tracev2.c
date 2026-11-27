// tracev2.c: the Go execution trace that runtime/trace reads (format
// version Go126), opt-in with MARMOT_TRACE_V2=1 while it is in progress.
//
// Each M writes its events into its own buffer, one EvEventBatch batch of
// the current generation. A generation ends under stop the world
// (sched_tv2_switch in sched.c): every buffer is closed and handed to the
// reader with the generation's stack table, and the statuses of every P and
// goroutine open the next one. lib/src/runtime/trace_marmot.go turns the
// stack table into the strings and stacks batches, adds the sync batch and
// serves ReadTrace.
//
// Who writes into a buffer:
// - An M that holds a running P writes its own buffer without a lock. The
//   switch runs only while that M is stopped, and every write runs with
//   m->locks > 0, so no stop lands inside a write. The check of tv2_on and
//   the state change an event reports happen in that same region, so a
//   switch sees the state change with its event or neither.
// - An M without a P (a syscall exit, sysmon, a poller) runs beside the
//   switch. It takes tv2_plock around the state change and its event; the
//   switch holds tv2_plock while it closes buffers and writes statuses.
// - The switching M writes the statuses of stopped Ms into their buffers.

#include "internal.h"

_Atomic uint32_t tv2_on;
_Atomic uint64_t tv2_gen;
rt_spin tv2_plock;

static rt_spin buf_lock;  // the free and full lists, the buffer allocator and the stack tables
static fixalloc buf_alloc;
static bool buf_inited;
static tv2buf *buf_free;
static tv2buf *full_head, *full_tail;

// ---- encoding ----

static uint8_t *uv(uint8_t *p, uint64_t v) {
  while (v >= 0x80) {
    *p++ = (uint8_t)(v | 0x80);
    v >>= 7;
  }
  *p++ = (uint8_t)v;
  return p;
}

// The batch length goes in 4 bytes reserved at the batch start: a uvarint
// padded with continuation bits, as gc writes it (readers accept it).
static void put_reserved(uint8_t *p, uint32_t v) {
  for (int i = 0; i < 3; i++) {
    p[i] = (uint8_t)((v & 0x7f) | 0x80);
    v >>= 7;
  }
  p[3] = (uint8_t)v;
}

// The trace clock: nanoseconds / 64, as gc's on Linux (EvFrequency 15625000).
uint64_t tv2_now(void) { return (uint64_t)os_nanotime() >> 6; }

// The kernel thread ID where there is one, else one from the M's address
// (sysmon and m0 may both have id 0). Never ^0, which means no M.
uint64_t tv2_mid(rt_m *m) {
  if (!m) return TV2_NOM;
  return m->tid > 0 ? (uint64_t)m->tid : ((uint64_t)(uintptr_t)m >> 4) | ((uint64_t)1 << 40);
}

// ---- buffers ----

static tv2buf *buf_get(void) {
  spin_lock(&buf_lock);
  if (!buf_inited) {
    fixalloc_init(&buf_alloc, sizeof(tv2buf));
    buf_inited = true;
  }
  tv2buf *b = buf_free;
  if (b)
    buf_free = b->next;
  else
    b = fixalloc_alloc(&buf_alloc);
  spin_unlock(&buf_lock);
  b->next = NULL;
  return b;
}

// For trace_marmot.go: the bytes of batch b, and the batch after it.
RT_API uint8_t *rt_tv2_buf(tv2buf *b, int32_t *len, tv2buf **next) {
  *len = (int32_t)b->len;
  *next = b->next;
  return b->data;
}

RT_API void rt_tv2_free(tv2buf *b) {
  spin_lock(&buf_lock);
  b->next = buf_free;
  buf_free = b;
  spin_unlock(&buf_lock);
}

static void batch_open(rt_m *m, tv2buf **slot, uint64_t gen) {
  tv2buf *b = buf_get();
  uint64_t ts = tv2_now();
  uint64_t last = m ? m->tv2_ts : 0;
  if (ts <= last) ts = last + 1;
  b->gen = gen;
  b->last = ts;
  uint8_t *p = b->data;
  *p++ = TV2_EV_EVENT_BATCH;
  p = uv(p, gen);
  p = uv(p, tv2_mid(m));
  p = uv(p, ts);
  b->lenat = (uint32_t)(p - b->data);
  p += 4;
  b->len = b->start = (uint32_t)(p - b->data);
  *slot = b;
}

// Closes a batch: an empty one goes back to the free list, the others to
// the full list of their generation.
static void batch_close(tv2buf *b) {
  if (b->len == b->start) {
    rt_tv2_free(b);
    return;
  }
  put_reserved(b->data + b->lenat, b->len - b->start);
  spin_lock(&buf_lock);
  b->next = NULL;
  if (full_tail)
    full_tail->next = b;
  else
    full_head = b;
  full_tail = b;
  spin_unlock(&buf_lock);
}

// The batches of the generations closed so far, oldest first.
tv2buf *tv2_take_full(void) {
  spin_lock(&buf_lock);
  tv2buf *b = full_head;
  full_head = full_tail = NULL;
  spin_unlock(&buf_lock);
  return b;
}

// Buffers of Ms that have no rt_m (a thread the runtime does not know).
static tv2buf *nom_buf;
static uint64_t nom_ts;

void tv2_close_m(rt_m *m) {
  tv2buf **slot = m ? &m->tv2b : &nom_buf;
  if (*slot) batch_close(*slot);
  *slot = NULL;
}

// One event of type t with n arguments after the timestamp, in m's batch
// of the current generation.
void tv2_ev(rt_m *m, uint8_t t, int n, uint64_t a0, uint64_t a1, uint64_t a2, uint64_t a3) {
  uint64_t gen = atomic_load_explicit(&tv2_gen, memory_order_relaxed);
  tv2buf **slot = m ? &m->tv2b : &nom_buf;
  tv2buf *b = *slot;
  if (b && b->gen != gen) rt_throw("tracev2: a batch of an old generation is open");
  if (!b || b->len + TV2_EVMAX > sizeof b->data) {
    if (b) batch_close(b);
    batch_open(m, slot, gen);
    b = *slot;
  }
  uint64_t ts = tv2_now();
  if (ts <= b->last) ts = b->last + 1;
  uint8_t *p = b->data + b->len;
  *p++ = t;
  p = uv(p, ts - b->last);
  uint64_t a[4] = {a0, a1, a2, a3};
  for (int i = 0; i < n; i++) p = uv(p, a[i]);
  b->len = (uint32_t)(p - b->data);
  b->last = ts;
  if (m)
    m->tv2_ts = ts;
  else
    nom_ts = ts;
}

// ---- stack tables ----
//
// One table per generation, two in use: the current one and the last
// closed one, which trace_marmot.go reads before it asks for the next
// switch. Each stack is a list of return addresses; its ID is its index + 1.

#define TV2_HASH (1u << 16)          // slots in the hash index
#define TV2_MAXSTACKS (TV2_HASH / 4 * 3)
#define TV2_CHUNK 256                // stacks per chunk

typedef struct {
  uint64_t hash;
  uint32_t n;
  uintptr_t pc[TV2_FRAMES];
} tv2stack;

typedef struct {
  uint32_t *index;                   // TV2_HASH slots: stack ID, 0 if empty
  tv2stack *chunks[TV2_MAXSTACKS / TV2_CHUNK + 1];
  uint32_t n;
} tv2table;

static tv2table tables[2];

static uint64_t stack_hash(const uintptr_t *pc, int32_t n) {
  uint64_t h = 1469598103934665603ull;
  for (int32_t i = 0; i < n; i++) {
    h ^= (uint64_t)pc[i];
    h *= 1099511628211ull;
    h ^= h >> 29;
  }
  return h ^ (uint64_t)n;
}

// The ID of the stack pc[0..n) in the current generation's table, 0 for
// none (no frames, or the table is full).
uint64_t tv2_stack_id(const uintptr_t *pc, int32_t n) {
  if (n <= 0) return 0;
  if (n > TV2_FRAMES) n = TV2_FRAMES;
  uint64_t h = stack_hash(pc, n);
  spin_lock(&buf_lock);
  tv2table *t = &tables[atomic_load_explicit(&tv2_gen, memory_order_relaxed) & 1];
  if (!t->index) t->index = os_map(TV2_HASH * sizeof(uint32_t));
  uint64_t id = 0;
  for (uint32_t i = (uint32_t)h & (TV2_HASH - 1);; i = (i + 1) & (TV2_HASH - 1)) {
    uint32_t k = t->index[i];
    if (!k) {
      if (t->n >= TV2_MAXSTACKS) break;
      uint32_t c = t->n / TV2_CHUNK;
      if (!t->chunks[c]) t->chunks[c] = os_map(TV2_CHUNK * sizeof(tv2stack));
      tv2stack *s = &t->chunks[c][t->n % TV2_CHUNK];
      s->hash = h;
      s->n = (uint32_t)n;
      __builtin_memcpy(s->pc, pc, (uintptr_t)n * sizeof(uintptr_t));
      t->index[i] = ++t->n;
      id = t->n;
      break;
    }
    tv2stack *s = &t->chunks[(k - 1) / TV2_CHUNK][(k - 1) % TV2_CHUNK];
    if (s->hash == h && s->n == (uint32_t)n &&
        __builtin_memcmp(s->pc, pc, (uintptr_t)n * sizeof(uintptr_t)) == 0) {
      id = k;
      break;
    }
  }
  spin_unlock(&buf_lock);
  return id;
}

// Empties the table that generation gen will use (it held gen - 2).
void tv2_table_reset(uint64_t gen) {
  spin_lock(&buf_lock);
  tv2table *t = &tables[gen & 1];
  if (t->index && t->n) __builtin_memset(t->index, 0, TV2_HASH * sizeof(uint32_t));
  t->n = 0;
  spin_unlock(&buf_lock);
}

// For trace_marmot.go: the number of stacks of generation gen (the last
// closed one), and the frames of stack id.
RT_API int32_t rt_tv2_nstacks(uint64_t gen) { return (int32_t)tables[gen & 1].n; }

RT_API int32_t rt_tv2_stack(uint64_t gen, int32_t id, uintptr_t *pc) {
  tv2table *t = &tables[gen & 1];
  if (id < 1 || (uint32_t)id > t->n) return 0;
  tv2stack *s = &t->chunks[(uint32_t)(id - 1) / TV2_CHUNK][(uint32_t)(id - 1) % TV2_CHUNK];
  __builtin_memcpy(pc, s->pc, s->n * sizeof(uintptr_t));
  return (int32_t)s->n;
}

// The frames above fp while fp stays in [lo, hi).
int32_t tv2_walk(uintptr_t first, uintptr_t fp, uintptr_t lo, uintptr_t hi, uintptr_t *pc) {
  int32_t n = 0;
  if (first) pc[n++] = first;
  while (n < TV2_FRAMES && fp >= lo && fp + 16 <= hi && !(fp & 7)) {
    uintptr_t up = ((uintptr_t *)fp)[0], ret = ((uintptr_t *)fp)[1];
    if (!ret) break;
    pc[n++] = ret;
    if (up <= fp) break;
    fp = up;
  }
  return n;
}

// The stack of the running goroutine g above frame fp.
uint64_t tv2_stack_here(rt_g *g, void *fp) {
  if (!g || !g->stack_hi) return 0;
  uintptr_t pc[TV2_FRAMES];
  int32_t n = tv2_walk(0, (uintptr_t)fp, g->stack_lo, g->stack_hi, pc);
  return tv2_stack_id(pc, n);
}

// The stack of g, switched out (its context saved at g->sp).
uint64_t tv2_stack_parked(rt_g *g) {
  if (!g->sp || !g->stack_hi) return 0;
  uintptr_t *f = g->sp, pc[TV2_FRAMES];
#if RT_ARCH_ARM64
  int32_t n = tv2_walk(f[11], f[10], g->stack_lo, g->stack_hi, pc);
#else
  int32_t n = tv2_walk(f[6], f[5], g->stack_lo, g->stack_hi, pc);
#endif
  return tv2_stack_id(pc, n);
}

// The stack of a goroutine that has not run yet: its start function, as
// gc writes it (the PC after the entry, so that a reader's pc-1 lands in it).
uint64_t tv2_stack_start(rt_g *g) {
  uintptr_t pc = (uintptr_t)g->fn + 1;
  return g->fn ? tv2_stack_id(&pc, 1) : 0;
}

// The clock snapshot of the generation that starts now.
static tv2clock clock_now;

void tv2_clock_snapshot(void) {
  int64_t sec;
  int32_t nsec;
  os_walltime(&sec, &nsec);
  clock_now.mono = os_nanotime();
  clock_now.ts = (uint64_t)clock_now.mono >> 6;
  clock_now.sec = sec;
  clock_now.nsec = nsec;
}

tv2clock tv2_clock(void) { return clock_now; }

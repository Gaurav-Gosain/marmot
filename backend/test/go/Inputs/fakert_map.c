// Map and channel part of the fake runtime: maps from int64 to int64 with a
// linear table, channels with one buffered int64 slot.
#include <stdio.h>
#include <stdlib.h>
#include <stdbool.h>
#ifdef __APPLE__
#define SYM(name) __asm__("_runtime." name)
#else
#define SYM(name) __asm__("runtime." name)
#endif

typedef struct { long n; long keys[64]; long vals[64]; } map;
static long zero;
static long *find(map *m, long k) {
  for (long i = 0; i < m->n; i++)
    if (m->keys[i] == k) return &m->vals[i];
  return NULL;
}
map *makemap(void *t, long hint) SYM("makemap");
map *makemap(void *t, long hint) { (void)t; (void)hint; return calloc(1, sizeof(map)); }
long *mapaccess1(void *t, map *m, long *k) SYM("mapaccess1");
long *mapaccess1(void *t, map *m, long *k) {
  long *p = m ? find(m, *k) : NULL;
  return p ? p : &zero;
}
typedef struct { long *p; bool ok; } res2;
res2 mapaccess2(void *t, map *m, long *k) SYM("mapaccess2");
res2 mapaccess2(void *t, map *m, long *k) {
  long *p = m ? find(m, *k) : NULL;
  res2 r = {p ? p : &zero, p != NULL};
  return r;
}
long *mapassign(void *t, map *m, long *k) SYM("mapassign");
long *mapassign(void *t, map *m, long *k) {
  long *p = find(m, *k);
  if (p) return p;
  m->keys[m->n] = *k;
  return &m->vals[m->n++];
}
void mapdelete(void *t, map *m, long *k) SYM("mapdelete");
void mapdelete(void *t, map *m, long *k) {
  long *p = find(m, *k);
  if (!p) return;
  long i = p - m->vals;
  m->keys[i] = m->keys[m->n - 1];
  m->vals[i] = m->vals[m->n - 1];
  m->n--;
}

typedef struct { int full; int closed; long v; } chan;
chan *makechan(void *t, long size) SYM("makechan");
chan *makechan(void *t, long size) { (void)t; (void)size; return calloc(1, sizeof(chan)); }
void chansend1(chan *c, long *v) SYM("chansend1");
void chansend1(chan *c, long *v) {
  if (c->full) { printf("deadlock\n"); exit(2); }
  c->v = *v;
  c->full = 1;
}
bool chanrecv2(chan *c, long *v) SYM("chanrecv2");
bool chanrecv2(chan *c, long *v) {
  if (!c->full) { *v = 0; return false; }
  *v = c->v;
  c->full = 0;
  return true;
}
void chanrecv1(chan *c, long *v) SYM("chanrecv1");
void chanrecv1(chan *c, long *v) { chanrecv2(c, v); }

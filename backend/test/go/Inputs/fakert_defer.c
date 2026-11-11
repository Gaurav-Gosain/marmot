// Defer/panic/recover part of the fake runtime, following the protocol in
// lib/src/runtime/panic.go with a single goroutine.
#include <stdio.h>
#include <stdlib.h>
#ifdef __APPLE__
#define SYM(name) __asm__("_runtime." name)
#else
#define SYM(name) __asm__("runtime." name)
#endif

typedef struct { void *tab, *data; } iface;
struct defer { struct defer *link; void *fn; void *frame; char wrapped; };
static struct defer *chain;
static int panicking, recovered;
static iface panicval;
void rt_longjmp(void *buf, int val);

static void callfn(void *closure) {
  void (*code)(void *) = *(void (**)(void *))closure;
  code(closure);
}

void deferpush(struct defer *d) SYM("deferpush");
void deferpush(struct defer *d) { d->link = chain; chain = d; }
void deferreturn(void *fr) SYM("deferreturn");
void deferreturn(void *fr) {
  while (chain && chain->frame == fr) {
    struct defer *d = chain;
    chain = d->link;
    callfn(d->fn);
  }
}
void gopanic(iface v) SYM("gopanic");
void gopanic(iface v) {
  panicking = 1;
  panicval = v;
  while (chain) {
    struct defer *d = chain;
    chain = d->link;
    recovered = 0;
    callfn(d->fn);
    if (recovered) {
      panicking = 0;
      rt_longjmp(d->frame, 1);
    }
  }
  printf("panic: %ld\n", (long)v.data);
  exit(2);
}
iface gorecover(long fp) SYM("gorecover");
iface gorecover(long fp) {
  iface nil = {0, 0};
  if (!fp) { printf("bad fp\n"); exit(3); }
  if (!panicking || recovered) return nil;
  recovered = 1;
  return panicval;
}
void newproc(void *fn) SYM("newproc");
void newproc(void *fn) { callfn(fn); }
void *mallocConservative(long n) SYM("mallocConservative");
void *mallocConservative(long n) { return calloc(1, n); }

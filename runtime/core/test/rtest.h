// rtest.h: helpers for runtime core tests.
#ifndef RTEST_H
#define RTEST_H
#include "rt.h"
#include <string.h>

#define CHECK(c)                                                      \
  do {                                                                \
    if (!(c)) {                                                       \
      static const char m_[] = __FILE__ ":" RT_STR(__LINE__) ": check failed: " #c; \
      rt_fatal(m_, sizeof m_ - 1);                                    \
    }                                                                 \
  } while (0)
#define RT_STR(x) RT_STR2(x)
#define RT_STR2(x) #x

static inline void out(const char *s) { rt_write(1, s, strlen(s)); }
static inline void outu(unsigned long long v) {
  char b[24]; int i = 24;
  do { b[--i] = (char)('0' + v % 10); v /= 10; } while (v);
  rt_write(1, b + i, (uintptr_t)(24 - i));
}

// A tiny wait group on rt_park/rt_ready.
typedef struct {
  rt_mutex mu;
  int n;
  rt_g *waiter;
} waitgroup;

static void wg_unlock(void *p) { rt_unlock(&((waitgroup *)p)->mu); }

static inline void wg_add(waitgroup *w, int n) {
  rt_lock(&w->mu); w->n += n; rt_unlock(&w->mu);
}
static inline void wg_done(waitgroup *w) {
  rt_lock(&w->mu);
  rt_g *g = 0;
  if (--w->n == 0 && w->waiter) { g = w->waiter; w->waiter = 0; }
  rt_unlock(&w->mu);
  if (g) rt_ready(g);
}
static inline void wg_wait(waitgroup *w) {
  rt_lock(&w->mu);
  if (w->n == 0) { rt_unlock(&w->mu); return; }
  w->waiter = rt_current();
  rt_park(wg_unlock, w);
}
#endif

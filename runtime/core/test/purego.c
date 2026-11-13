// purego calls: rt_purego_syscall with integer and float arguments (more
// than fit in registers), two-register and float results, errno on darwin;
// and a C callback through rt_purego_callback_entry into a stand-in for the
// Go side, with register and stack arguments, from inside a call and from
// threads the runtime does not know.
// expect-stdout: ok
#include "rtest.h"
#include <errno.h>
#include <pthread.h>
#include <stdint.h>

void rt_purego_syscall(uintptr_t *a);
void *rt_purego_callback_entry(intptr_t i);

enum { FN = 0, A1 = 1, F1 = 33, NWORDS = 42 };  // r8 is word 41

static uintptr_t fbits(double d) {
  uintptr_t u;
  memcpy(&u, &d, 8);
  return u;
}
static double fval(uintptr_t u) {
  double d;
  memcpy(&d, &u, 8);
  return d;
}

// Twelve integers (some on the stack on both arches) and two doubles.
static long mix(long a1, long a2, long a3, long a4, long a5, long a6, long a7, long a8, long a9,
                long a10, long a11, long a12, double f1, double f2) {
  return a1 + 2 * a2 + 3 * a3 + 4 * a4 + 5 * a5 + 6 * a6 + 7 * a7 + 8 * a8 + 9 * a9 + 10 * a10 +
         11 * a11 + 12 * a12 + (long)(f1 * 100) + (long)(f2 * 1000);
}

static double fmix(double a, long b, double c) { return a * (double)b + c; }

typedef struct {
  long x, y;
} pair;
static pair two(long a, long b) { return (pair){a + 1, b + 2}; }

static long set_errno(long e) {
  errno = (int)e;
  return -1;
}

// The callback's C signature: nine integers (the last on the stack on
// arm64, the last three on amd64) and two doubles.
typedef long (*cbfn)(long, long, long, long, long, long, long, long, long, double, double);

static intptr_t seen_index = -1;

// The Go side of a callback, as purego's callbackWrap sees it: the block is
// f0..f7, the integer registers, then the stack arguments.
void rt_purego_go_callback(uintptr_t *args) {
  seen_index = (intptr_t)args[0];
  uintptr_t *frame = (uintptr_t *)args[1];
  long ints[9];
  for (int i = 0; i < 9; i++) ints[i] = (long)frame[8 + i];  // registers, then the stack, contiguous
  double d0 = fval(frame[0]), d1 = fval(frame[1]);
  long r = 0;
  for (int i = 0; i < 9; i++) r = r * 3 + ints[i];
  args[2] = (uintptr_t)(r + (long)(d0 * 10) + (long)(d1 * 100));  // result[0]
}

static long call_back(cbfn f) { return f(1, 2, 3, 4, 5, 6, 7, 8, 9, 0.5, 0.25); }

static long expect_cb(void);

// A thread the runtime does not know calls the callback (attached for the
// call, detached after).
static void *foreign(void *e) {
  for (int i = 0; i < 20; i++) CHECK(call_back((cbfn)e) == expect_cb());
  return 0;
}

static long expect_cb(void) {
  long r = 0;
  for (int i = 1; i <= 9; i++) r = r * 3 + i;
  return r + 5 + 25;
}

void marmot_main(void) {
  uintptr_t a[NWORDS];

  memset(a, 0, sizeof a);
  a[FN] = (uintptr_t)mix;
  for (int i = 0; i < 12; i++) a[A1 + i] = (uintptr_t)(i + 1);
  a[F1] = fbits(1.5);
  a[F1 + 1] = fbits(0.25);
  rt_purego_syscall(a);
  CHECK((long)a[A1] == mix(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 1.5, 0.25));

  memset(a, 0, sizeof a);
  a[FN] = (uintptr_t)fmix;
  a[A1] = 4;
  a[F1] = fbits(2.5);
  a[F1 + 1] = fbits(0.5);
  rt_purego_syscall(a);
  CHECK(fval(a[F1]) == 10.5);

  memset(a, 0, sizeof a);
  a[FN] = (uintptr_t)two;
  a[A1] = 10;
  a[A1 + 1] = 20;
  rt_purego_syscall(a);
  CHECK(a[A1] == 11 && a[A1 + 1] == 22);

  memset(a, 0, sizeof a);
  a[FN] = (uintptr_t)set_errno;
  a[A1] = EAGAIN;
  rt_purego_syscall(a);
#if defined(__APPLE__)
  CHECK(a[A1 + 2] == EAGAIN);
#else
  CHECK(a[A1 + 2] == 0);
#endif

  CHECK(rt_purego_callback_entry(2000) == 0);
  void *e3 = rt_purego_callback_entry(3);
  CHECK(e3 != 0 && e3 != rt_purego_callback_entry(4));
  for (int k = 0; k < 100; k++) {
    memset(a, 0, sizeof a);
    a[FN] = (uintptr_t)call_back;
    a[A1] = (uintptr_t)e3;
    rt_purego_syscall(a);
    CHECK(seen_index == 3);
    CHECK((long)a[A1] == expect_cb());
    rt_gc_collect();
  }
  pthread_t t[2];
  for (int i = 0; i < 2; i++) CHECK(pthread_create(&t[i], 0, foreign, e3) == 0);
  rt_cgo_enter();
  for (int i = 0; i < 2; i++) pthread_join(t[i], 0);
  rt_cgo_exit();
  out("ok\n");
}

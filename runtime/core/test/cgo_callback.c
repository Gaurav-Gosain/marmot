// cgo callbacks: C called from a goroutine (quick and bracketed calls)
// calls back into Go, which blocks and runs other goroutines; the goroutine
// must come back on the same thread. Then C threads unknown to the runtime
// call Go twice each (attach, block, collect, detach).
// expect-stdout: ok
// wasm: skip (needs libc headers)
#include "rtest.h"
#include <pthread.h>
#include <stdatomic.h>

static _Atomic int ran;
static void worker(void *arg) {
  for (int i = 0; i < 1000; i++) rt_gosched();
  atomic_fetch_add(&ran, 1);
  wg_done(arg);
}

// The Go side of a callback: start goroutines and wait for them (parks).
static void go_side(int n) {
  waitgroup wg = {0};
  wg_add(&wg, n);
  for (int i = 0; i < n; i++) rt_go(worker, &wg);
  wg_wait(&wg);
}

static void callback(void) {
  pthread_t self = pthread_self();
  int32_t tok = rt_cgo_callback_enter();
  go_side(8);
  rt_gc_collect();
  for (int i = 0; i < 100; i++) rt_gosched();
  CHECK(pthread_equal(self, pthread_self()));
  rt_cgo_callback_exit(tok);
  CHECK(pthread_equal(self, pthread_self()));
}

// C code called from Go that calls back.
static void c_func(void) { callback(); }

static void *foreign(void *arg) {
  callback();
  callback();
  return 0;
}

void marmot_main(void) {
  rt_gomaxprocs(4);
  for (int k = 0; k < 20; k++) {
    rt_cgo_enter_fast();
    c_func();
    rt_cgo_exit_fast();
    rt_cgo_enter();
    c_func();
    rt_cgo_exit();
  }
  CHECK(ran == 20 * 2 * 8);
  pthread_t t[4];
  for (int i = 0; i < 4; i++) CHECK(pthread_create(&t[i], 0, foreign, 0) == 0);
  rt_cgo_enter();
  for (int i = 0; i < 4; i++) pthread_join(t[i], 0);
  rt_cgo_exit();
  CHECK(ran == 20 * 2 * 8 + 4 * 2 * 8);
  out("ok\n");
}

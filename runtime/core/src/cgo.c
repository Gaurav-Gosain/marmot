// cgo.c: entry and exit of C calls from cgo packages (internal/cgo).
//
// A cgo call is a direct native call on the goroutine stack: the stack is
// large and committed on use, so C needs no stack switch. The generated C
// wrapper of each C function brackets the call:
//
//   rt_cgo_enter / rt_cgo_exit: rt_entersyscall and rt_exitsyscall, so a
//   C function that blocks does not hold its P, and m->incgo.
//   rt_cgo_enter_fast / rt_cgo_exit_fast: m->incgo only, for functions that
//   return quickly (libc leaf functions, #cgo noescape plus nocallback).
//   Stop the world still reaches such a call: SIGURG stops the thread at
//   any pc and the GC scans its stack, C frames included, conservatively.
//
// m->incgo counts the C calls the M is in; a fault while it is set is
// fatal, as in gc, instead of a Go panic unwinding through C frames.
// rt_cgo_exit_errno keeps errno in the goroutine before rt_exitsyscall,
// which may resume the goroutine on another thread.

#include "internal.h"

RT_API void rt_cgo_enter(void) {
  rt_entersyscall();
  rt_m *m = getm();
  if (m) m->incgo++;
}

RT_API void rt_cgo_exit(void) {
  rt_m *m = getm();
  if (m) m->incgo--;
  rt_exitsyscall();
}

RT_API void rt_cgo_enter_fast(void) {
  rt_m *m = getm();
  if (m) m->incgo++;
}

RT_API void rt_cgo_exit_fast(void) {
  rt_m *m = getm();
  if (m) m->incgo--;
}

RT_API void rt_cgo_exit_errno(int32_t e) {
  rt_m *m = getm();
  if (m) {
    m->incgo--;
    if (m->curg) m->curg->cgo_errno = e;
  }
  rt_exitsyscall();
}

// rt_cgo_errno is errno after the last C call of the goroutine.
RT_API int32_t rt_cgo_errno(void) {
  rt_m *m = getm();
  return m && m->curg ? m->curg->cgo_errno : 0;
}

// rt_cgo_keep keeps a pointer argument of a C call live in the Go frame
// until the call returns: while a goroutine is in a bracketed C call the
// collector scans its stack above the entry point only, not the C frames.
RT_API __attribute__((noinline)) void rt_cgo_keep(void *p) { __asm__ volatile("" ::"r"(p) : "memory"); }

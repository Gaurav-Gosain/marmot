// lock.c: rt_mutex for the Go runtime, internal spin locks, notes.

#include "internal.h"

// Lock word states.
enum { UNLOCKED = 0, LOCKED = 1, CONTENDED = 2 };

INLINE void cpu_relax(void) {
#if RT_ARCH_ARM64
  __asm__ volatile("yield");
#else
  __asm__ volatile("pause");
#endif
}

static void lock_slow(_Atomic uint32_t *v) {
  for (int spin = 0; spin < 64; spin++) {
    uint32_t s = atomic_load_explicit(v, memory_order_relaxed);
    if (s == UNLOCKED &&
        atomic_compare_exchange_weak_explicit(v, &s, LOCKED, memory_order_acquire,
                                              memory_order_relaxed))
      return;
    cpu_relax();
  }
  // Mark contended and sleep until it is released.
  while (atomic_exchange_explicit(v, CONTENDED, memory_order_acquire) != UNLOCKED)
    os_futex_wait(v, CONTENDED, -1);
}

INLINE void lock_word(_Atomic uint32_t *v) {
  uint32_t s = UNLOCKED;
  if (LIKELY(atomic_compare_exchange_weak_explicit(v, &s, LOCKED, memory_order_acquire,
                                                   memory_order_relaxed)))
    return;
  lock_slow(v);
}

INLINE void unlock_word(_Atomic uint32_t *v) {
  if (UNLIKELY(atomic_exchange_explicit(v, UNLOCKED, memory_order_release) == CONTENDED))
    os_futex_wake(v, false);
}

// rt_mutex: a waiting M stays stoppable for GC (no m->locks), so a waiter
// sleeping in the kernel does not block stop-the-world.
RT_API void rt_lock(rt_mutex *m) { lock_word((_Atomic uint32_t *)&m->state); }
RT_API void rt_unlock(rt_mutex *m) { unlock_word((_Atomic uint32_t *)&m->state); }

void spin_lock(rt_spin *l) {
  rt_m *m = rt_tls_m;
  if (m) atomic_fetch_add_explicit(&m->locks, 1, memory_order_relaxed);
  lock_word(&l->v);
}

void spin_unlock(rt_spin *l) {
  unlock_word(&l->v);
  rt_m *m = rt_tls_m;
  if (m) atomic_fetch_sub_explicit(&m->locks, 1, memory_order_relaxed);
}

// ---- notes ----

void noteclear(note *n) { atomic_store_explicit(&n->key, 0, memory_order_relaxed); }

void notewakeup(note *n) {
  uint32_t old = atomic_exchange_explicit(&n->key, 1, memory_order_release);
  if (old != 0) rt_throw("notewakeup: double wakeup");
  os_futex_wake(&n->key, true);
}

void notesleep(note *n) {
  while (atomic_load_explicit(&n->key, memory_order_acquire) == 0)
    os_futex_wait(&n->key, 0, -1);
}

bool notetsleep(note *n, int64_t ns) {
  if (ns < 0) {
    notesleep(n);
    return true;
  }
  int64_t deadline = os_nanotime() + ns;
  while (atomic_load_explicit(&n->key, memory_order_acquire) == 0) {
    int64_t left = deadline - os_nanotime();
    if (left <= 0) return false;
    os_futex_wait(&n->key, 0, left);
  }
  return true;
}

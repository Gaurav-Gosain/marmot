// arch_wasm32.c: contexts (goroutine switching) on wasm32 with Asyncify.
//
// WebAssembly has no way to switch stacks: the call stack of the engine
// is not memory. Binaryen's Asyncify pass (wasm-opt --asyncify, run at
// link time) rewrites the program so that a call chain can unwind into a
// buffer and later rewind from it. A context is such a buffer plus a
// shadow stack (the part of the C stack in linear memory, whose pointer is
// the __stack_pointer global).
//
// Every context runs on top of ctx_run, the base of the engine's stack.
// rt_ctx_switch records where the next context is, saves the shadow stack
// pointer and starts an unwind: the frames of the current context save
// their locals in its buffer and return, down to ctx_run. ctx_run then
// rewinds the next context: it calls that context's entry function again,
// and the frames restore their locals from the buffer, back to the point
// where that context switched away.
//
// Link flags (wasm-opt), as the driver and the tests use them, before any
// optimization pass (an optimizer may inline ctx_run first):
//   --asyncify --pass-arg=asyncify-imports@asyncify.start_unwind
//   --pass-arg=asyncify-removelist@ctx_run
// Only calls that may reach rt_ctx_switch (direct, or through an indirect
// call) are instrumented.
//
// The stack of a goroutine [lo, hi) holds, from the top: the context, the
// Asyncify buffer (the upper half) and the shadow stack, growing down from
// the middle. The saved stack pointer points to a word holding the
// context, below everything that is live, so a conservative scan of
// [saved sp, hi) covers the shadow stack and the buffer.

#include "internal.h"

#if RT_ARCH_WASM32

#define ASYNCIFY(name) __attribute__((import_module("asyncify"), import_name(#name)))
ASYNCIFY(start_unwind) void asyncify_start_unwind(void *data);
ASYNCIFY(stop_unwind) void asyncify_stop_unwind(void);
ASYNCIFY(start_rewind) void asyncify_start_rewind(void *data);
ASYNCIFY(stop_rewind) void asyncify_stop_rewind(void);

typedef struct wctx {
  uintptr_t cur, end;  // Asyncify's data: next free byte and end of the buffer (first)
  uintptr_t start;     // start of the buffer
  uintptr_t top;       // end of what the last unwind wrote
  void *sp;            // shadow stack pointer to resume with
  void (*fn)(void *);  // entry, called again to rewind
  void *arg;
  uint32_t started;
} wctx;

static wctx *cur;    // running
static wctx *next;   // to run after the unwind in progress
static uint32_t rewinding;

// The shadow stack pointer, a wasm global the linker defines.
__asm__(".globaltype __stack_pointer, i32");

static inline void *get_sp(void) {
  void *sp;
  __asm__ volatile("global.get __stack_pointer\n\tlocal.set %0" : "=r"(sp));
  return sp;
}

static inline void set_sp(void *sp) {
  __asm__ volatile("local.get %0\n\tglobal.set __stack_pointer" ::"r"(sp));
}

// The saved stack pointer: a word below sp holding the context (the
// context resumes with sp). Only ctx_run runs on that stack before the
// switch back, and it uses no shadow stack.
static void *saved_sp(wctx *c, void *sp) {
  wctx **slot = (wctx **)(((uintptr_t)sp - 16) & ~(uintptr_t)15);
  *slot = c;
  c->sp = sp;
  return slot;
}

static wctx *ctx_of(void *saved) { return *(wctx **)saved; }

void *rt_ctx_init_wasm(uintptr_t lo, uintptr_t hi, void (*fn)(void *), void *arg) {
  wctx *c = (wctx *)((hi - sizeof(wctx)) & ~(uintptr_t)15);
  uintptr_t mid = (lo + ((uintptr_t)c - lo) / 2) & ~(uintptr_t)15;
  c->start = c->cur = c->top = mid;
  c->end = (uintptr_t)c;
  c->fn = fn;
  c->arg = arg;
  c->started = 0;
  return saved_sp(c, (void *)mid);
}

// Other targets get only the top; on wasm every caller knows the bottom
// (sched.c calls rt_ctx_init_wasm).
void *rt_ctx_init(void *top, void (*fn)(void *), void *arg) {
  rt_throw("rt_ctx_init on wasm needs the stack bottom");
}

// Unwind the current context (its c->sp set) to ctx_run, which then runs
// to.
static NOINLINE void unwind_to(wctx *to) {
  wctx *c = cur;
  c->cur = c->start;
  next = to;
  asyncify_start_unwind(c);
}

NOINLINE void rt_ctx_switch(void **save, void *to) {
  if (rewinding) {  // resumed
    rewinding = 0;
    asyncify_stop_rewind();
    return;
  }
  *save = saved_sp(cur, get_sp());
  unwind_to(ctx_of(to));
}

// Unwind and rewind the current context, so that the locals of every frame
// are in its buffer, above the returned pointer, for a conservative scan.
NOINLINE void *rt_spill_regs(void *buf) {
  if (rewinding) {
    rewinding = 0;
    asyncify_stop_rewind();
    return (void *)(((uintptr_t)get_sp() - 16) & ~(uintptr_t)15);
  }
  saved_sp(cur, get_sp());
  unwind_to(cur);
  return NULL;  // unwinding: not seen
}

// The base of the engine's stack (not instrumented). Runs c, then whatever
// each unwind names, for ever: an entry function never returns.
NOINLINE NORETURN void ctx_run(wctx *c) {
  for (;;) {
    cur = c;
    set_sp(c->sp);
    if (c->started) {
      rewinding = 1;
      asyncify_start_rewind(c);
    }
    c->started = 1;
    c->fn(c->arg);
    asyncify_stop_unwind();
    // Clear what an earlier, deeper unwind left above this one, so the
    // conservative scan does not keep its pointers alive.
    if (c->cur < c->top) __builtin_memset((void *)c->cur, 0, c->top - c->cur);
    c->top = c->cur;
    c = next;
  }
}

// ---- setjmp and longjmp (defer frames, panics) ----
//
// The compiler lowers setjmp with wasm exceptions (llc -wasm-enable-sjlj,
// legacy encoding: wasm-opt's Asyncify does not take the new one yet): a
// function that calls setjmp catches the __c_longjmp exception, and these
// helpers (as in wasi-libc) record and test which setjmp call a buffer
// belongs to. rt_setjmp is that setjmp on wasm (rt.h).

__asm__(".globl __c_longjmp\n.tagtype __c_longjmp i32\n__c_longjmp:\n");

typedef struct {
  void *fid;  // the setjmp caller's invocation
  uint32_t label;
} jmp_env;

typedef struct {
  jmp_env *env;
  int32_t val;
} jmp_arg;

RT_API void __wasm_setjmp(void *env, uint32_t label, void *fid) {
  jmp_env *e = env;
  e->fid = fid;
  e->label = label;
}

RT_API uint32_t __wasm_setjmp_test(void *env, void *fid) {
  jmp_env *e = env;
  return e->fid == fid ? e->label : 0;
}

__attribute__((target("exception-handling"))) RT_API NORETURN void __wasm_longjmp(void *env, int32_t val) {
  static jmp_arg arg;  // one thread; read by the catch at once
  arg.env = env;
  arg.val = val ? val : 1;
  __builtin_wasm_throw(1, &arg);
  __builtin_unreachable();
}

RT_API void rt_longjmp(void *buf, int32_t val) { __wasm_longjmp(buf, val); }

// The first context: the program's own stack (the scheduler runs on it as
// m0's g0), with a buffer in the data segment.
static uint8_t main_buf[32 << 10] __attribute__((aligned(16)));
static wctx main_ctx;

void rt_ctx_start(void (*fn)(void *), void *arg) {
  main_ctx.start = main_ctx.cur = main_ctx.top = (uintptr_t)main_buf;
  main_ctx.end = (uintptr_t)main_buf + sizeof main_buf;
  saved_sp(&main_ctx, get_sp());
  main_ctx.fn = fn;
  main_ctx.arg = arg;
  ctx_run(&main_ctx);
}

#endif

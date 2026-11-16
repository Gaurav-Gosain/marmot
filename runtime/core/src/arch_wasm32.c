// arch_wasm32.c: contexts (goroutine switching) on wasm32 with Asyncify,
// or with JSPI in the wasip1-wasm-jspi library (RT_WASM_JSPI, below).
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

#if !RT_WASM_JSPI

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

#else // RT_WASM_JSPI

// ---- JSPI (RT_WASM_JSPI=1, the wasip1-wasm-jspi library) ----
//
// With JavaScript Promise Integration the engine switches stacks: every
// goroutine runs on a stack of its own, entered through the export
// marmot_gorun, which the host wraps with WebAssembly.promising. A switch is
// a call to the import marmot.switch(from, to), which the host wraps with
// WebAssembly.Suspending: it suspends the calling stack (from) and starts or
// resumes to. The program's _start must be wrapped with promising too (the
// scheduler and the main goroutine run on its stack). No Asyncify pass.
//
// Host contract (test/wasm/jspi.mjs has one):
//   switch(from, to, fresh): remember how to resume from; if fresh, drop
//     any stack held for to and call marmot_gorun(to) on a new one, else
//     resume to's stack; return a promise that settles when some stack
//     switches back to from.
//   marmot_gorun(ctx) never returns: a goroutine's entry ends in a switch.
//
// Engine stacks are pooled: a new engine stack costs about 10 us, a
// switch well under 1. When a goroutine exits (REQ_EXIT), its engine stack
// suspends under its estack record, not its context, and waits in a pool.
// A goroutine that has not run yet takes a pooled stack when there is one:
// the stack resumes in the exit switch and longjmps to its loop
// (jspi_gorun_loop), which runs the new context. The longjmp unwinds the
// frames of the exited goroutine; their handlers test only the target
// buffer (in the estack), so they read none of the shadow stack memory,
// which may be another goroutine's by then.
//
// The suspended stacks are invisible to a scan, so every pointer that is
// live across a call must be in linear memory: the program is linked, then
// rewritten with wasm-opt --spill-pointers (each frame stores its pointer
// locals to its shadow stack frame before a call). The collector scans each
// goroutine's shadow stack [saved sp, hi), as with Asyncify, minus the
// buffer: the whole stack is shadow stack.

struct estack;
NORETURN void __wasm_longjmp(void *env, int32_t val);
// src/jspi_wasm32.c: setjmp(jb), then run(); run never returns.
void jspi_gorun_loop(void *jb, void (*run)(void));

typedef struct wctx {
  void *sp;            // shadow stack pointer to resume with
  void (*fn)(void *);  // entry
  void *arg;
  uint32_t started;    // 0 until an engine stack runs it
  struct estack *es;   // the engine stack it runs on (NULL: _start's)
} wctx;

// One engine stack entered through marmot_gorun. Never freed: there are at
// most as many as goroutines ever ran at once.
typedef struct estack {
  void *jb[4];                 // jspi_gorun_loop's setjmp buffer
  wctx *next;                  // the context to run next on this stack
  struct estack *link;         // pool
  uint8_t frame[256];          // jspi_gorun_loop's shadow stack frame
} estack;

#define ESTACK_POOL_MAX 256
static estack *es_pool;
static int32_t es_npool;
static estack *es_run;  // the engine stack whose loop runs its next context
static wctx *cur;       // running

// fresh: to has not run yet. A context at the address of an exited
// goroutine's (stacks are reused) is a new one, and the host drops the old
// suspended stack.
__attribute__((import_module("marmot"), import_name("switch"))) void jspi_switch(wctx *from, wctx *to, uint32_t fresh);

__asm__(".globaltype __stack_pointer, i32");

static inline void *get_sp(void) {
  void *sp;
  __asm__ volatile("global.get __stack_pointer\n\tlocal.set %0" : "=r"(sp));
  return sp;
}

static inline void set_sp(void *sp) {
  __asm__ volatile("local.get %0\n\tglobal.set __stack_pointer" ::"r"(sp));
}

// As with Asyncify: a word below sp holds the context.
static void *saved_sp(wctx *c, void *sp) {
  wctx **slot = (wctx **)(((uintptr_t)sp - 16) & ~(uintptr_t)15);
  *slot = c;
  c->sp = sp;
  return slot;
}

static wctx *ctx_of(void *saved) { return *(wctx **)saved; }

void *rt_ctx_init_wasm(uintptr_t lo, uintptr_t hi, void (*fn)(void *), void *arg) {
  wctx *c = (wctx *)((hi - sizeof(wctx)) & ~(uintptr_t)15);
  c->fn = fn;
  c->arg = arg;
  c->started = 0;
  c->es = NULL;
  return saved_sp(c, (void *)((uintptr_t)c & ~(uintptr_t)15));
}

void *rt_ctx_init(void *top, void (*fn)(void *), void *arg) {
  rt_throw("rt_ctx_init on wasm needs the stack bottom");
}

// Suspend the current engine stack under key and run t: resume it, or
// start it on a pooled engine stack, or on a new one. exiting, when not
// NULL, joins the pool once a pooled stack has been taken for t.
static void run_ctx(wctx *key, wctx *t, estack *exiting) {
  uint32_t fresh = 0;
  wctx *to = t;
  if (!t->started) {
    t->started = 1;
    estack *es = es_pool;
    if (es) {
      es_pool = es->link;
      es_npool--;
      to = (wctx *)es;  // resumed in the exit switch below
    } else {
      es = persistent_alloc(sizeof(estack), 16);
      fresh = 1;  // the host calls marmot_gorun(t)
    }
    es->next = t;
    t->es = es;
  }
  if (exiting) {
    exiting->link = es_pool;
    es_pool = exiting;
    es_npool++;
  }
  jspi_switch(key, to, fresh);
}

NOINLINE void rt_ctx_switch(void **save, void *to) {
  wctx *c = cur;
  void *slot = saved_sp(c, get_sp());
  *save = slot;
  // What runs on this engine stack until it suspends (the host's start of
  // another stack's marmot_gorun) allocates below the slot.
  set_sp(slot);
  wctx *t = ctx_of(to);
  estack *es = c->es;
  if (es && getm()->req == REQ_EXIT && es_npool < ESTACK_POOL_MAX) {
    // c is done: its engine stack waits in the pool under es.
    c->es = NULL;
    run_ctx((wctx *)es, t, es);
    // Taken for a new context: back to the loop of this stack.
    es_run = es;
    __wasm_longjmp(es->jb, 1);
  }
  run_ctx(c, t, NULL);
  // Resumed: other stacks ran and moved the stack pointer.
  cur = c;
  set_sp(c->sp);
}

// The body of the loop of engine stack es_run: run its next context.
static NORETURN void gorun_next(void) {
  wctx *c = es_run->next;
  cur = c;
  set_sp(c->sp);
  c->fn(c->arg);
  rt_throw("goroutine entry returned");
}

// A new engine stack for c (the host calls it, wrapped with promising).
// Nothing is live across the call, so --spill-pointers gives it no frame on
// the stack it was entered from. The loop's shadow frame is es->frame:
// goroutine stacks are reused when their goroutines exit, the loop stays.
__attribute__((export_name("marmot_gorun"))) NORETURN void marmot_gorun(wctx *c) {
  estack *es = c->es;
  es_run = es;
  set_sp(es->frame + sizeof es->frame);
  jspi_gorun_loop(es->jb, gorun_next);
  rt_throw("goroutine loop returned");
}

// The pointers of every frame are on the shadow stack already
// (--spill-pointers), below the caller's stack pointer.
NOINLINE void *rt_spill_regs(void *buf) { return (void *)(((uintptr_t)get_sp() - 16) & ~(uintptr_t)15); }

#endif // RT_WASM_JSPI

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

#if !RT_WASM_JSPI

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

#else

// The first context: the program's own stack, _start's.
static wctx main_ctx;

void rt_ctx_start(void (*fn)(void *), void *arg) {
  saved_sp(&main_ctx, get_sp());
  main_ctx.fn = fn;
  main_ctx.arg = arg;
  main_ctx.started = 1;
  cur = &main_ctx;
  fn(arg);
  rt_throw("main context entry returned");
}

#endif

#endif

// jspi_wasm32.c: the loop of a JSPI engine stack (src/arch_wasm32.c). Built
// only for the wasip1-wasm-jspi library (the Makefile leaves it out
// elsewhere). A file of its own: it calls setjmp, which the compiler lowers
// with wasm exceptions, against the __wasm_setjmp helpers of arch_wasm32.c.
#include "internal.h"

#if RT_WASM_JSPI

void jspi_gorun_loop(void *jb, void (*run)(void));

// setjmp, then run(), which never returns. A goroutine that exits on this
// engine stack longjmps to jb when the stack is taken for a new goroutine,
// and run() runs that one.
void jspi_gorun_loop(void *jb, void (*run)(void)) {
  setjmp(jb);
  run();
}

#endif

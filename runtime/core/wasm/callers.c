// wasm-callers.o: the host imports behind tracebacks on wasm, linked only
// when the host provides them (the browser playground: the driver links
// this object with MARMOT_WASM_HOSTCALLERS=1). Without it the runtime's
// weak references are null and wasm panics print no frames, as before.
//
//   marmot.callers(buf, max) -> n: module byte offsets of the wasm frames
//     on the stack, innermost first, the host's own frames left out.
//   marmot.pcln(ptr, cap) -> len: the module's ".marmot_pcln" section;
//     when cap is too small, copies nothing and returns len.
#include <stdint.h>

__attribute__((import_module("marmot"), import_name("callers"))) int32_t marmot_callers(uint32_t *buf, int32_t max);
__attribute__((import_module("marmot"), import_name("pcln"))) int32_t marmot_pcln(void *ptr, int32_t cap);

int32_t rt_wasm_host_callers(uint32_t *buf, int32_t max) { return marmot_callers(buf, max); }
int32_t rt_wasm_host_pcln(void *ptr, int32_t cap) { return marmot_pcln(ptr, cap); }

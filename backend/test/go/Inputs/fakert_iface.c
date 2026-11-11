// Interface part of the fake runtime (see fakert.c): the runtime functions the
// lowering calls, with their Go symbol names.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef __APPLE__
#define SYM(name) __asm__("_runtime." name)
#define SYM_TYPE(name) __asm__("_type:" name)
#else
#define SYM(name) __asm__("runtime." name)
#define SYM_TYPE(name) __asm__("type:" name)
#endif

// Interfaces. The test module defines one itab, test_itab, for (*main.T,
// main.I), and the descriptor "type:*main.T".
extern char test_itab[];
extern char type_ptr_T[] SYM_TYPE("*main.T");
void *rt_assertE2I(void *inter, void *t) SYM("assertE2I");
void *rt_assertE2I(void *inter, void *t) {
  (void)inter;
  if (t != type_ptr_T) { printf("panic: missing method\n"); exit(2); }
  return test_itab;
}
void *rt_assertE2I2(void *inter, void *t) SYM("assertE2I2");
void *rt_assertE2I2(void *inter, void *t) {
  (void)inter;
  return t == type_ptr_T ? test_itab : NULL;
}
void rt_panicTypeAssert(void *have, void *want, void *iface) SYM("panicTypeAssert");
void rt_panicTypeAssert(void *have, void *want, void *iface) {
  printf("panic: interface conversion: have %s, want %s\n",
         have == type_ptr_T ? "*main.T" : have ? "other" : "nil",
         want == type_ptr_T ? "*main.T" : "other");
  (void)iface;
  exit(2);
}

// A fake Go runtime for backend run tests: the runtime functions the
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

void *rt_newobject(void *t) SYM("newobject");
void *rt_newobject(void *t) { (void)t; return calloc(1, 64); }
void *rt_makeslice(void *t, long len, long cap) SYM("makeslice");
void *rt_makeslice(void *t, long len, long cap) {
  (void)t;
  if (len < 0 || cap < len) { printf("panic: makeslice\n"); exit(2); }
  return calloc(cap ? cap : 1, 8);
}
void rt_panicindex(long i, long n) SYM("panicindex");
void rt_panicindex(long i, long n) {
  printf("panic: index out of range [%ld] with length %ld\n", i, n);
  exit(2);
}
void rt_panicslice(long lo, long hi, long cap) SYM("panicslice");
void rt_panicslice(long lo, long hi, long cap) {
  printf("panic: slice bounds out of range [%ld:%ld] with capacity %ld\n", lo, hi, cap);
  exit(2);
}
void rt_panicnil(void) SYM("panicnil");
void rt_panicnil(void) {
  printf("panic: nil pointer dereference\n");
  exit(2);
}

long test_main(long argc);
int main(int argc, char **argv) {
  printf("result %ld\n", test_main(argc > 1 ? atol(argv[1]) : 0));
  return 0;
}


// Linked with dead stripping, the type link table keeps only the entry
// whose descriptor is live (get_live); the dead descriptor's entry goes.
#include <stdint.h>
#include <stdio.h>
#ifdef __APPLE__
#include <mach-o/getsect.h>
#include <mach-o/ldsyms.h>
#else
extern void *__start_marmot_tlinks[] __attribute__((weak, visibility("hidden")));
extern void *__stop_marmot_tlinks[] __attribute__((weak, visibility("hidden")));
#endif
void *get_live(void);

int main(void) {
  void **e;
  long n;
#ifdef __APPLE__
  unsigned long size = 0;
  e = (void **)getsectiondata(&_mh_execute_header, "__DATA", "__marmot_tlinks", &size);
  n = e ? (long)(size / sizeof(void *)) : 0;
#else
  e = __start_marmot_tlinks;
  n = e ? __stop_marmot_tlinks - __start_marmot_tlinks : 0;
#endif
  if (n != 1 || e[0] != get_live()) {
    printf("tlinks: %ld entries, first %s\n", n, n && e[0] == get_live() ? "live" : "wrong");
    return 1;
  }
  return 0;
}

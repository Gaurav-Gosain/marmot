#include <stdint.h>
#include <stdio.h>
extern int64_t np_data, np_bss[4], np_zero[64];
int64_t np_bump(void);
int main(void) {
  if (np_data != 5 || np_bss[0] || np_bss[3] || np_zero[0] || np_zero[63]) { puts("init: bad"); return 1; }
  if (np_bump() != 8 || np_bump() != 11 || np_data != 7 || np_bss[3] != 4) { puts("bump: bad"); return 1; }
  return 0;
}

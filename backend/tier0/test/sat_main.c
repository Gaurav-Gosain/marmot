#include <stdint.h>
#include <stdio.h>
#include <math.h>
int32_t sat32(double); int32_t sat32f(float); int8_t sat8(double);
uint32_t usat32(double); uint16_t usat16f(float);
static int bad;
#define CK(a,b) do{ long long g_=(long long)(a), w_=(long long)(b); if(g_!=w_){printf("%s: %lld != %lld\n",#a,g_,w_);bad=1;} }while(0)
int main(void) {
  CK(sat32(1e30), INT32_MAX); CK(sat32(-1e30), INT32_MIN); CK(sat32(NAN), 0); CK(sat32(-3.7), -3);
  CK(sat32(2147483647.9), INT32_MAX); CK(sat32(2147483648.0), INT32_MAX); CK(sat32(-2147483648.9), INT32_MIN);
  CK(sat32(-2147483649.0), INT32_MIN); CK(sat32(123456.5), 123456); CK(sat32(INFINITY), INT32_MAX);
  CK(sat32f(3e9f), INT32_MAX); CK(sat32f(-3e9f), INT32_MIN); CK(sat32f(NAN), 0); CK(sat32f(-7.5f), -7);
  CK(sat8(300), 127); CK(sat8(-300), -128); CK(sat8(-5.9), -5); CK(sat8(NAN), 0);
  CK(usat32(5e9), UINT32_MAX); CK(usat32(-1), 0); CK(usat32(NAN), 0); CK(usat32(4294967295.0), UINT32_MAX);
  CK(usat32(3000000000.5), 3000000000u);
  CK(usat16f(70000.f), 65535); CK(usat16f(-2.f), 0); CK(usat16f(12.9f), 12); CK(usat16f(-INFINITY), 0);
  return bad;
}

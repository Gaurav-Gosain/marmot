#include <stdint.h>
#include <stdio.h>
#include <math.h>
uint64_t clz64(uint64_t); uint8_t clz8(uint8_t); uint32_t ctz32(uint32_t); uint64_t ctz64(uint64_t);
uint64_t pop64(uint64_t); uint32_t bswap32(uint32_t); uint16_t brev16(uint16_t);
uint64_t fshl64(uint64_t,uint64_t,uint64_t); uint32_t fshl32(uint32_t,uint32_t,uint32_t);
uint8_t fshr8(uint8_t,uint8_t,uint8_t); uint64_t fshr64(uint64_t,uint64_t,uint64_t);
double fmaxi(double,double); float fmini(float,float); double fmaxn(double,double);
int64_t sat(double); uint64_t usat(double); void *fa(void);
static int bad;
#define CK(a,b) do{ unsigned long long g_=(unsigned long long)(a), w_=(unsigned long long)(b); if(g_!=w_){printf("%s: %llx != %llx\n",#a,g_,w_);bad=1;} }while(0)
static uint16_t rb16(uint16_t v){uint16_t r=0;for(int i=0;i<16;i++)if(v>>i&1)r|=1<<(15-i);return r;}
static uint64_t rf64(uint64_t a,uint64_t b,unsigned s,int l){s&=63;if(!s)return l?a:b;return l?(a<<s|b>>(64-s)):(a<<(64-s)|b>>s);}
static uint32_t rf32(uint32_t a,uint32_t b,unsigned s){s&=31;if(!s)return a;return a<<s|b>>(32-s);}
static uint8_t rr8(uint8_t a,uint8_t b,unsigned s){s&=7;if(!s)return b;return (uint8_t)(a<<(8-s)|b>>s);}
int main(void){
  uint64_t v[]={0,1,2,3,0x80,0xff,0x1234,0x80000000,0xffffffff,0x8000000000000000ull,0xdeadbeefcafebabeull,~0ull};
  for(int i=0;i<12;i++){ uint64_t x=v[i];
    CK(clz64(x), x?__builtin_clzll(x):64);
    CK(clz8((uint8_t)x), (uint8_t)x?__builtin_clz((uint8_t)x)-24:8);
    CK(ctz32((uint32_t)x), (uint32_t)x?__builtin_ctz((uint32_t)x):32);
    CK(ctz64(x), x?__builtin_ctzll(x):64);
    CK(pop64(x), __builtin_popcountll(x));
    CK(bswap32((uint32_t)x), __builtin_bswap32((uint32_t)x));
    CK(brev16((uint16_t)x), rb16((uint16_t)x));
    for(unsigned s=0;s<70;s+=7){
      CK(fshl64(x,~x^0x5555,s), rf64(x,~x^0x5555,s,1));
      CK(fshr64(x,~x^0x5555,s), rf64(x,~x^0x5555,s,0));
      CK(fshl32((uint32_t)x,0x9abcdef0,s), rf32((uint32_t)x,0x9abcdef0,s));
      CK(fshr8((uint8_t)x,0x5a,s), rr8((uint8_t)x,0x5a,s));
    }
  }
  CK(sat(1e30), INT64_MAX); CK(sat(-1e30), INT64_MIN); CK(sat(NAN), 0); CK(sat(-3.7), -3);
  CK(usat(1e30), UINT64_MAX); CK(usat(-5), 0); CK(usat(NAN), 0);
  CK(fmaxi(1, 2) == 2, 1); CK(isnan(fmaxi(NAN, 2)), 1); CK(signbit(fmaxi(-0.0, 0.0)), 0);
  CK(fmini(1, 2) == 1, 1); CK(isnan(fmini(3, NAN)), 1); CK(signbit(fmini(0.0f, -0.0f)), 1);
  CK(fmaxn(NAN, 2) == 2, 1); CK(fmaxn(-1, -3) == -1, 1);
  CK(fa(), fa()); if(!fa()) bad=1;
  puts(bad?"FAIL":"ok"); return bad;
}

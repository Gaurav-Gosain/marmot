#include <stdio.h>
#include <stdint.h>
typedef unsigned __int128 u; typedef __int128 s;
static int bad;
void w_add(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_sub(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_mul(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_and(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_or(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_xor(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_lshr(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_ashr(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_shl(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t*);
void w_k(uint64_t*);
int main(void){ uint64_t v[]={0,1,3,63,64,65,100,127,0x8000000000000000ull,~0ull,0xdeadbeefcafebabeull}; uint64_t r[2];
 for(int i=0;i<11;i++)for(int j=0;j<11;j++)for(int k=0;k<11;k++){ u x=((u)v[j]<<64)|v[i]; u y=((u)(uint64_t)v[k]<<64)|v[(i+k)%11]; uint64_t cc=v[(i+k)%11], dd=v[k]; u e;
  if(!(y>127 && (0))){ w_add(v[i],v[j],cc,dd,r); e=x+y; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("add %d %d %d\n",i,j,k);bad=1;} }
  if(!(y>127 && (0))){ w_sub(v[i],v[j],cc,dd,r); e=x-y; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("sub %d %d %d\n",i,j,k);bad=1;} }
  if(!(y>127 && (0))){ w_mul(v[i],v[j],cc,dd,r); e=x*y; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("mul %d %d %d\n",i,j,k);bad=1;} }
  if(!(y>127 && (0))){ w_and(v[i],v[j],cc,dd,r); e=x&y; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("and %d %d %d\n",i,j,k);bad=1;} }
  if(!(y>127 && (0))){ w_or(v[i],v[j],cc,dd,r); e=x|y; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("or %d %d %d\n",i,j,k);bad=1;} }
  if(!(y>127 && (0))){ w_xor(v[i],v[j],cc,dd,r); e=x^y; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("xor %d %d %d\n",i,j,k);bad=1;} }
  if(!(y>127 && (1))){ w_lshr(v[i],v[j],cc,dd,r); e=(y&127)==y?x>>y:0; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("lshr %d %d %d\n",i,j,k);bad=1;} }
  if(!(y>127 && (1))){ w_ashr(v[i],v[j],cc,dd,r); e=(y&127)==y?(u)((s)x>>y):0; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("ashr %d %d %d\n",i,j,k);bad=1;} }
  if(!(y>127 && (1))){ w_shl(v[i],v[j],cc,dd,r); e=(y&127)==y?x<<y:0; if(r[0]!=(uint64_t)e||r[1]!=(uint64_t)(e>>64)){printf("shl %d %d %d\n",i,j,k);bad=1;} }
 }
 w_k(r); s kk=-(s)(((u)1<<127)-1); if(r[0]!=(uint64_t)kk||r[1]!=(uint64_t)((u)kk>>64)){puts("const");bad=1;}
 puts(bad?"FAIL":"ok"); return bad; }
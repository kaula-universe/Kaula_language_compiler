#include <stdio.h>
#include <time.h>
#include <stdint.h>
#define MULT 1103515245LL
#define ADD 12345LL
#define MASK30 1073741824LL
#define MOD_P 9223372036854775807LL
static int64_t parse(void){int64_t s=2545,c=0;for(int k=0;k<256;k++){s=((s%1048576)*MULT+ADD)%MOD_P;c=((c%MASK30)*31+(s%256))%MOD_P;}return c;}
static int64_t degrade(int64_t spin){int64_t acc=1,r=0;while(r<spin){acc=((acc%MASK30)*MULT+ADD)%MOD_P;r++;}return acc^parse();}
static uint64_t now(void){struct timespec t;clock_gettime(CLOCK_MONOTONIC,&t);return t.tv_sec*1000000000ULL+t.tv_nsec;}
int main(void){
  int N=200000; volatile int64_t sink=0;
  for(int64_t spin=5000;spin<=40000;spin+=5000){
    uint64_t t0=now(); for(int i=0;i<N;i++) sink=degrade(spin); uint64_t dt=now()-t0;
    printf("spin=%-7lld 单任务=%-8.2f us  32核上限=%-7.2f M TPS\n",(long long)spin,dt/1000.0/N,32.0*N*1e9/dt/1e6);
  }
}

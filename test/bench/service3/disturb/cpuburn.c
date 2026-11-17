#define _GNU_SOURCE
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <pthread.h>
static volatile unsigned long sink;
static void* burn(void* arg){
    long cpu = (long)arg;
    cpu_set_t s; CPU_ZERO(&s); CPU_SET(cpu, &s);
    sched_setaffinity(0, sizeof(s), &s);
    unsigned long x = 0x9E3779B97F4A7C15UL;
    for(;;){ x ^= x<<13; x ^= x>>7; x ^= x<<17; x *= 2654435761UL; sink = x; }
    return NULL;
}
int main(int argc, char** argv){
    int n = argc>1 ? atoi(argv[1]) : 8;
    int base = argc>2 ? atoi(argv[2]) : 16;
    pthread_t t;
    for(int i=0;i<n;i++) pthread_create(&t, NULL, burn, (void*)(long)((base+i) % sysconf(_SC_NPROCESSORS_ONLN)));
    for(;;) pause();
    return 0;
}

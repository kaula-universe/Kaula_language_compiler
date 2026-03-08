#include "kaula.h"

static inline uint64_t get_clock_cycles() {
    LARGE_INTEGER counter;
    QueryPerformanceCounter(&counter);
    return counter.QuadPart;
}

static inline double get_clock_frequency() {
    LARGE_INTEGER frequency;
    QueryPerformanceFrequency(&frequency);
    return (double)frequency.QuadPart;
}
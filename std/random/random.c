#include "random.h"
#include "../memory/memory.h"
#include "../system/system.h"
#include <stdlib.h>
#include <time.h>
#include <math.h>

static u64 g_seed = 1;

struct RandomGenerator {
    u64 state;
};

static u64 splitmix64(u64* x) {
    u64 z = (*x += 0x9e3779b97f4a7c15);
    z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9;
    z = (z ^ (z >> 27)) * 0x94d049bb133111eb;
    return z ^ (z >> 31);
}

static u64 xorshift128p(u64* s0, u64* s1) {
    u64 t = *s0;
    u64 s = *s1;
    *s0 = s;
    t ^= t << 23;
    t ^= t >> 17;
    t ^= s ^ (s >> 26);
    *s1 = t;
    return t + s;
}

static u64 g_s0 = 0;
static u64 g_s1 = 0;

void random_init(void) {
    random_seed_from_time();
}

void random_seed(u64 seed) {
    g_seed = seed;
    g_s0 = splitmix64(&g_seed);
    g_s1 = splitmix64(&g_seed);
}

void random_seed_from_time(void) {
    random_seed((u64)time(NULL) ^ (u64)(clock() << 32));
}

u32 random_u32(void) {
    return (u32)xorshift128p(&g_s0, &g_s1);
}

u64 random_u64(void) {
    return xorshift128p(&g_s0, &g_s1);
}

i32 random_i32(void) {
    return (i32)random_u32();
}

i64 random_i64(void) {
    return (i64)random_u64();
}

u32 random_range_u32(u32 min, u32 max) {
    if (min >= max) return min;
    return min + (random_u32() % (max - min));
}

u64 random_range_u64(u64 min, u64 max) {
    if (min >= max) return min;
    return min + (random_u64() % (max - min));
}

i32 random_range_i32(i32 min, i32 max) {
    if (min >= max) return min;
    u32 range = (u32)(max - min);
    return min + (i32)(random_u32() % range);
}

i64 random_range_i64(i64 min, i64 max) {
    if (min >= max) return min;
    u64 range = (u64)(max - min);
    return min + (i64)(random_u64() % range);
}

f32 random_f32(void) {
    return (f32)(random_u32() * (1.0f / 4294967296.0f));
}

f64 random_f64(void) {
    u64 x = random_u64();
    return (f64)(x >> 11) * (1.0 / (1LL << 53));
}

f32 random_range_f32(f32 min, f32 max) {
    return min + (max - min) * random_f32();
}

f64 random_range_f64(f64 min, f64 max) {
    return min + (max - min) * random_f64();
}

bool_t random_bool(void) {
    return (random_u32() & 1) == 1;
}

bool_t random_chance(f64 probability) {
    if (probability <= 0.0) return false;
    if (probability >= 1.0) return true;
    return random_f64() < probability;
}

void random_bytes(u8* buffer, size_t len) {
    for (size_t i = 0; i < len; i++) {
        buffer[i] = (u8)random_u32();
    }
}

RandomGenerator* random_generator_create(u64 seed) {
    RandomGenerator* gen = (RandomGenerator*)kmm_v4_malloc(sizeof(RandomGenerator));
    if (!gen) return NULL;
    gen->state = seed;
    return gen;
}

void random_generator_destroy(RandomGenerator* gen) {
    if (gen) kmm_v4_free(gen);
}

u64 random_generator_next(RandomGenerator* gen) {
    if (!gen) return 0;
    return splitmix64(&gen->state);
}

f64 random_generator_next_f64(RandomGenerator* gen) {
    u64 x = random_generator_next(gen);
    return (f64)(x >> 11) * (1.0 / (1LL << 53));
}

u64 random_uuid(void) {
    u64 a = random_u64();
    u64 b = random_u64();
    return (a << 32) | (b >> 32);
}

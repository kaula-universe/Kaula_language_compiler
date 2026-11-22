#ifndef STD_RANDOM_RANDOM_H
#define STD_RANDOM_RANDOM_H

#include "../base/types.h"

typedef struct RandomGenerator RandomGenerator;

void random_init(void);
void random_seed(u64 seed);
void random_seed_from_time(void);

u32 random_u32(void);
u64 random_u64(void);
i32 random_i32(void);
i64 random_i64(void);

u32 random_range_u32(u32 min, u32 max);
u64 random_range_u64(u64 min, u64 max);
i32 random_range_i32(i32 min, i32 max);
i64 random_range_i64(i64 min, i64 max);

f32 random_f32(void);
f64 random_f64(void);
f32 random_range_f32(f32 min, f32 max);
f64 random_range_f64(f64 min, f64 max);

bool_t random_bool(void);
bool_t random_chance(f64 probability);

void random_bytes(u8* buffer, size_t len);

RandomGenerator* random_generator_create(u64 seed);
void random_generator_destroy(RandomGenerator* gen);
u64 random_generator_next(RandomGenerator* gen);
f64 random_generator_next_f64(RandomGenerator* gen);

u64 random_uuid(void);

#endif

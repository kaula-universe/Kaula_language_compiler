#ifndef STD_DECIMAL_DECIMAL_H
#define STD_DECIMAL_DECIMAL_H

#include "../base/types.h"

typedef struct Decimal Decimal;

Decimal* decimal_create(f64 value);
Decimal* decimal_create_from_string(const char* str);
void decimal_destroy(Decimal* dec);

Decimal* decimal_add(const Decimal* a, const Decimal* b);
Decimal* decimal_subtract(const Decimal* a, const Decimal* b);
Decimal* decimal_multiply(const Decimal* a, const Decimal* b);
Decimal* decimal_divide(const Decimal* a, const Decimal* b);
Decimal* decimal_negate(const Decimal* dec);

Decimal* decimal_abs(const Decimal* dec);
Decimal* decimal_floor(const Decimal* dec);
Decimal* decimal_ceil(const Decimal* dec);
Decimal* decimal_round(const Decimal* dec, i64 places);

i64 decimal_compare(const Decimal* a, const Decimal* b);
bool_t decimal_equal(const Decimal* a, const Decimal* b);
bool_t decimal_less(const Decimal* a, const Decimal* b);
bool_t decimal_greater(const Decimal* a, const Decimal* b);

f64 decimal_to_f64(const Decimal* dec);
i64 decimal_to_i64(const Decimal* dec);
char* decimal_to_string(const Decimal* dec);
char* decimal_to_string_with_precision(const Decimal* dec, i64 places);

Decimal* decimal_pow(const Decimal* base, i64 exp);
Decimal* decimal_sqrt(const Decimal* dec);

i64 decimal_get_precision(const Decimal* dec);
void decimal_set_precision(Decimal* dec, i64 precision);

#endif

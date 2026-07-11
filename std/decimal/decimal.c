#include "decimal.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include "../math/math.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <math.h>

struct Decimal {
    char* digits;
    i64 scale;
    i64 length;
    bool_t negative;
};

static void decimal_normalize(Decimal* dec);

Decimal* decimal_create(f64 value) {
    Decimal* dec = (Decimal*)kmm_v4_malloc(sizeof(Decimal));
    if (!dec) return NULL;
    
    dec->negative = value < 0;
    if (dec->negative) value = -value;
    
    i64 scale = 15;
    char buf[32];
    snprintf(buf, sizeof(buf), "%.*f", (int)scale, value);
    
    dec->length = (i64)strlen(buf);
    dec->digits = (char*)kmm_v4_malloc(dec->length + 1);
    strcpy(dec->digits, buf);
    dec->scale = scale;
    
    decimal_normalize(dec);
    return dec;
}

Decimal* decimal_create_from_string(const char* str) {
    Decimal* dec = (Decimal*)kmm_v4_malloc(sizeof(Decimal));
    if (!dec) return NULL;
    
    dec->negative = str[0] == '-';
    const char* start = dec->negative ? str + 1 : str;
    
    i64 len = (i64)strlen(start);
    dec->scale = 0;
    
    for (i64 i = 0; i < len; i++) {
        if (start[i] == '.') {
            dec->scale = len - i - 1;
            len = i;
            break;
        }
    }
    
    dec->length = len;
    dec->digits = (char*)kmm_v4_malloc(len + 1);
    strncpy(dec->digits, start, len);
    dec->digits[len] = '\0';
    
    decimal_normalize(dec);
    return dec;
}

void decimal_destroy(Decimal* dec) {
    if (!dec) return;
    kmm_v4_free(dec->digits);
    kmm_v4_free(dec);
}

static void decimal_normalize(Decimal* dec) {
    while (dec->length > 1 && dec->digits[0] == '0') {
        memmove(dec->digits, dec->digits + 1, dec->length);
        dec->length--;
    }
    if (dec->length == 0) {
        dec->digits[0] = '0';
        dec->length = 1;
        dec->negative = false;
    }
}

static void decimal_pad(Decimal* dec, i64 new_scale) {
    if (new_scale <= dec->scale) return;
    
    i64 diff = new_scale - dec->scale;
    char* new_digits = (char*)kmm_v4_malloc(dec->length + diff + 1);
    strcpy(new_digits, dec->digits);
    memset(new_digits + dec->length, '0', (size_t)diff);
    new_digits[dec->length + diff] = '\0';
    
    kmm_v4_free(dec->digits);
    dec->digits = new_digits;
    dec->length += diff;
    dec->scale = new_scale;
}

static Decimal* decimal_add_internal(const Decimal* a, const Decimal* b, bool_t subtract) {
    i64 max_scale = a->scale > b->scale ? a->scale : b->scale;
    
    Decimal* aa = decimal_create_from_string(a->digits);
    Decimal* bb = decimal_create_from_string(b->digits);
    decimal_pad(aa, max_scale);
    decimal_pad(bb, max_scale);
    
    bool_t a_neg = a->negative;
    bool_t b_neg = b->negative ^ subtract;
    
    i64 max_len = aa->length > bb->length ? aa->length : bb->length;
    char* result = (char*)kmm_v4_malloc(max_len + 2);
    memset(result, '0', (size_t)(max_len + 1));
    result[max_len + 1] = '\0';
    
    i64 carry = 0;
    for (i64 i = max_len - 1; i >= 0; i--) {
        i64 a_digit = i < aa->length ? (i64)(aa->digits[i] - '0') : 0;
        i64 b_digit = i < bb->length ? (i64)(bb->digits[i] - '0') : 0;
        
        if (a_neg != b_neg) {
            i64 diff = a_digit - b_digit + carry;
            if (diff < 0) {
                diff += 10;
                carry = -1;
            } else {
                carry = 0;
            }
            result[i] = (char)('0' + diff);
        } else {
            i64 sum = a_digit + b_digit + carry;
            result[i] = (char)('0' + (sum % 10));
            carry = sum / 10;
        }
    }
    
    Decimal* dec = (Decimal*)kmm_v4_malloc(sizeof(Decimal));
    dec->digits = result;
    dec->length = max_len;
    dec->scale = max_scale;
    dec->negative = a_neg;
    
    if (carry != 0) {
        char* new_result = (char*)kmm_v4_malloc(max_len + 2);
        new_result[0] = (char)('0' + carry);
        strcpy(new_result + 1, result);
        kmm_v4_free(result);
        dec->digits = new_result;
        dec->length++;
    }
    
    decimal_normalize(dec);
    decimal_destroy(aa);
    decimal_destroy(bb);
    
    return dec;
}

Decimal* decimal_add(const Decimal* a, const Decimal* b) {
    return decimal_add_internal(a, b, false);
}

Decimal* decimal_subtract(const Decimal* a, const Decimal* b) {
    return decimal_add_internal(a, b, true);
}

Decimal* decimal_multiply(const Decimal* a, const Decimal* b) {
    i64 len_a = a->length;
    i64 len_b = b->length;
    i64 result_len = len_a + len_b;
    
    i64* temp = (i64*)kmm_v4_malloc(result_len * sizeof(i64));
    memset(temp, 0, result_len * sizeof(i64));
    
    for (i64 i = len_a - 1; i >= 0; i--) {
        for (i64 j = len_b - 1; j >= 0; j--) {
            temp[i + j + 1] += (i64)(a->digits[i] - '0') * (i64)(b->digits[j] - '0');
        }
    }
    
    for (i64 i = result_len - 1; i > 0; i--) {
        temp[i - 1] += temp[i] / 10;
        temp[i] %= 10;
    }
    
    char* result = (char*)kmm_v4_malloc(result_len + 1);
    for (i64 i = 0; i < result_len; i++) {
        result[i] = (char)('0' + temp[i]);
    }
    result[result_len] = '\0';
    
    kmm_v4_free(temp);
    
    Decimal* dec = (Decimal*)kmm_v4_malloc(sizeof(Decimal));
    dec->digits = result;
    dec->length = result_len;
    dec->scale = a->scale + b->scale;
    dec->negative = a->negative != b->negative;
    
    decimal_normalize(dec);
    return dec;
}

Decimal* decimal_divide(const Decimal* a, const Decimal* b) {
    if (b->length == 1 && b->digits[0] == '0') {
        return decimal_create(0.0);
    }
    
    Decimal* result = decimal_create(0.0);
    Decimal* remainder = decimal_create_from_string(a->digits);
    remainder->negative = a->negative;
    remainder->scale = a->scale;
    
    i64 precision = 15;
    
    for (i64 i = 0; i <= precision; i++) {
        i64 count = 0;
        while (decimal_compare(remainder, b) >= 0) {
            Decimal* temp = decimal_subtract(remainder, b);
            decimal_destroy(remainder);
            remainder = temp;
            count++;
        }
        
        Decimal* digit_dec = decimal_create((f64)count);
        Decimal* shifted = decimal_multiply(result, decimal_create(10.0));
        decimal_destroy(result);
        result = decimal_add(shifted, digit_dec);
        decimal_destroy(shifted);
        decimal_destroy(digit_dec);
        
        if (i < precision) {
            Decimal* temp = decimal_multiply(remainder, decimal_create(10.0));
            decimal_destroy(remainder);
            remainder = temp;
        }
    }
    
    decimal_destroy(remainder);
    result->scale = precision;
    result->negative = a->negative != b->negative;
    
    return result;
}

Decimal* decimal_negate(const Decimal* dec) {
    Decimal* result = decimal_create_from_string(dec->digits);
    result->negative = !dec->negative;
    return result;
}

Decimal* decimal_abs(const Decimal* dec) {
    Decimal* result = decimal_create_from_string(dec->digits);
    result->negative = false;
    return result;
}

Decimal* decimal_floor(const Decimal* dec) {
    if (dec->scale == 0) {
        return decimal_create_from_string(dec->digits);
    }
    
    Decimal* result = decimal_create_from_string(dec->digits);
    result->scale = 0;
    
    if (dec->negative) {
        Decimal* one = decimal_create(1.0);
        result = decimal_subtract(result, one);
        decimal_destroy(one);
    }
    
    return result;
}

Decimal* decimal_ceil(const Decimal* dec) {
    if (dec->scale == 0) {
        return decimal_create_from_string(dec->digits);
    }
    
    Decimal* result = decimal_create_from_string(dec->digits);
    result->scale = 0;
    
    if (!dec->negative) {
        Decimal* one = decimal_create(1.0);
        result = decimal_add(result, one);
        decimal_destroy(one);
    }
    
    return result;
}

Decimal* decimal_round(const Decimal* dec, i64 places) {
    if (places >= dec->scale) {
        Decimal* result = decimal_create_from_string(dec->digits);
        decimal_pad(result, places);
        return result;
    }
    
    Decimal* result = decimal_create_from_string(dec->digits);
    i64 diff = result->scale - places;
    
    if (diff > 0 && (size_t)(result->length - diff) < result->length) {
        char round_digit = result->digits[result->length - diff];
        bool_t round_up = round_digit >= '5';
        
        result->length -= diff;
        result->scale = places;
        
        if (round_up) {
            Decimal* one = decimal_create(1.0);
            while (places > 0) {
                one = decimal_divide(one, decimal_create(10.0));
                places--;
            }
            result = decimal_add(result, one);
            decimal_destroy(one);
        }
    }
    
    return result;
}

i64 decimal_compare(const Decimal* a, const Decimal* b) {
    if (a->negative != b->negative) {
        return a->negative ? -1 : 1;
    }
    
    i64 max_scale = a->scale > b->scale ? a->scale : b->scale;
    Decimal* aa = decimal_create_from_string(a->digits);
    Decimal* bb = decimal_create_from_string(b->digits);
    decimal_pad(aa, max_scale);
    decimal_pad(bb, max_scale);
    
    i64 result = 0;
    if (aa->length != bb->length) {
        result = aa->length > bb->length ? 1 : -1;
    } else {
        result = strcmp(aa->digits, bb->digits);
    }
    
    decimal_destroy(aa);
    decimal_destroy(bb);
    
    return a->negative ? -result : result;
}

bool_t decimal_equal(const Decimal* a, const Decimal* b) {
    return decimal_compare(a, b) == 0;
}

bool_t decimal_less(const Decimal* a, const Decimal* b) {
    return decimal_compare(a, b) < 0;
}

bool_t decimal_greater(const Decimal* a, const Decimal* b) {
    return decimal_compare(a, b) > 0;
}

f64 decimal_to_f64(const Decimal* dec) {
    char* str = decimal_to_string(dec);
    f64 result = atof(str);
    kmm_v4_free(str);
    return dec->negative ? -result : result;
}

i64 decimal_to_i64(const Decimal* dec) {
    char* str = decimal_to_string(dec);
    i64 result = atoll(str);
    kmm_v4_free(str);
    return dec->negative ? -result : result;
}

char* decimal_to_string(const Decimal* dec) {
    return decimal_to_string_with_precision(dec, dec->scale);
}

char* decimal_to_string_with_precision(const Decimal* dec, i64 places) {
    Decimal* copy = decimal_create_from_string(dec->digits);
    copy->negative = dec->negative;
    decimal_pad(copy, places);
    
    i64 int_len = copy->length - places;
    if (int_len <= 0) int_len = 1;
    
    i64 total_len = int_len + (places > 0 ? (places + 1) : 0) + (copy->negative ? 1 : 0);
    char* result = (char*)kmm_v4_malloc(total_len + 1);
    
    i64 pos = 0;
    if (copy->negative) {
        result[pos++] = '-';
    }
    
    for (i64 i = 0; i < int_len; i++) {
        if (i < copy->length) {
            result[pos++] = copy->digits[i];
        } else {
            result[pos++] = '0';
        }
    }
    
    if (places > 0) {
        result[pos++] = '.';
        for (i64 i = int_len; i < int_len + places; i++) {
            if (i < copy->length) {
                result[pos++] = copy->digits[i];
            } else {
                result[pos++] = '0';
            }
        }
    }
    
    result[pos] = '\0';
    decimal_destroy(copy);
    return result;
}

Decimal* decimal_pow(const Decimal* base, i64 exp) {
    if (exp == 0) return decimal_create(1.0);
    
    bool_t neg_exp = exp < 0;
    if (neg_exp) exp = -exp;
    
    Decimal* result = decimal_create(1.0);
    Decimal* current = decimal_create_from_string(base->digits);
    current->negative = base->negative;
    
    while (exp > 0) {
        if (exp & 1) {
            Decimal* temp = decimal_multiply(result, current);
            decimal_destroy(result);
            result = temp;
        }
        Decimal* temp = decimal_multiply(current, current);
        decimal_destroy(current);
        current = temp;
        exp >>= 1;
    }
    
    decimal_destroy(current);
    
    if (neg_exp) {
        Decimal* temp = decimal_divide(decimal_create(1.0), result);
        decimal_destroy(result);
        result = temp;
    }
    
    return result;
}

Decimal* decimal_sqrt(const Decimal* dec) {
    if (dec->negative) {
        return decimal_create(0.0);
    }
    
    Decimal* low = decimal_create(0.0);
    Decimal* high = decimal_create_from_string(dec->digits);
    Decimal* epsilon = decimal_create(0.0000000000001);
    
    for (i64 iter = 0; iter < 100; iter++) {
        Decimal* mid = decimal_divide(decimal_add(low, high), decimal_create(2.0));
        Decimal* mid_sq = decimal_multiply(mid, mid);
        
        if (decimal_less(mid_sq, dec)) {
            decimal_destroy(low);
            low = mid;
        } else {
            decimal_destroy(high);
            high = mid;
        }
        decimal_destroy(mid_sq);
        
        Decimal* diff = decimal_subtract(high, low);
        bool_t done = decimal_less(diff, epsilon);
        decimal_destroy(diff);
        if (done) break;
    }
    
    Decimal* result = decimal_divide(decimal_add(low, high), decimal_create(2.0));
    decimal_destroy(low);
    decimal_destroy(high);
    decimal_destroy(epsilon);
    
    return result;
}

i64 decimal_get_precision(const Decimal* dec) {
    return dec->scale;
}

void decimal_set_precision(Decimal* dec, i64 precision) {
    decimal_pad(dec, precision);
}

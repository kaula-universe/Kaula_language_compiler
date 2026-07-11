#pragma once
#include "../base/types.h"
#include "../string/string.h"

// Option<T> - represents optional values
typedef struct {
    bool_t is_some;
    union {
        i64   as_i64;
        f64   as_f64;
        void* as_ptr;
        bool_t as_bool;
    } value;
} Option;

Option option_none(void);
Option option_some_i64(i64 val);
Option option_some_f64(f64 val);
Option option_some_ptr(void* val);
Option option_some_bool(bool_t val);

bool_t option_is_some(const Option* opt);
bool_t option_is_none(const Option* opt);
i64    option_unwrap_i64(const Option* opt);
f64    option_unwrap_f64(const Option* opt);
void*  option_unwrap_ptr(const Option* opt);
bool_t option_unwrap_bool(const Option* opt);

i64    option_unwrap_or_i64(const Option* opt, i64 default_val);
f64    option_unwrap_or_f64(const Option* opt, f64 default_val);
void*  option_unwrap_or_ptr(const Option* opt, void* default_val);
bool_t option_unwrap_or_bool(const Option* opt, bool_t default_val);

bool_t option_is_some_and(const Option* opt, bool_t (*pred)(const Option*));
Option option_map(const Option* opt, Option (*mapper)(const Option*));
Option option_and_then(const Option* opt, Option (*flat_mapper)(const Option*));
Option option_or(const Option* opt, Option alternative);
Option option_or_else(const Option* opt, Option (*fallback)(void));
void   option_take(Option* opt, Option* out);

// Result<T, E> - represents success or error
typedef enum {
    RESULT_OK,
    RESULT_ERR
} ResultStatus;

typedef struct {
    ResultStatus status;
    union {
        i64   ok_i64;
        f64   ok_f64;
        void* ok_ptr;
    } value;
    int    err_code;
    String err_msg;
} Result;

Result result_ok_i64(i64 val);
Result result_ok_f64(f64 val);
Result result_ok_ptr(void* val);
Result result_err(int code, const char* msg);

bool_t result_is_ok(const Result* r);
bool_t result_is_err(const Result* r);
i64    result_unwrap_ok_i64(const Result* r);
f64    result_unwrap_ok_f64(const Result* r);
void*  result_unwrap_ok_ptr(const Result* r);
int    result_err_code(const Result* r);
String result_err_msg(const Result* r);

Result result_map(const Result* r, Result (*mapper)(const Result*));
Result result_map_err(const Result* r, Result (*mapper)(const Result*));
Result result_and_then(const Result* r, Result (*flat_mapper)(const Result*));
Option result_to_option(const Result* r);
Result option_to_result(const Option* opt, int err_code, const char* err_msg);

void   result_destroy(Result* r);

// Macros for ergonomic usage
#define TRY(result_expr, err_target) \
    do { Result _r = (result_expr); if (_r.status == RESULT_ERR) { (err_target) = _r; return _r; } } while(0)

#define OPTION_SOME(val) option_some_i64((i64)(val))
#define OPTION_NONE option_none()

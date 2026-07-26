#include "option.h"
#include "../memory/memory.h"
#include <stdio.h>
#include <stdlib.h>

static void panic_none(const char* op) {
    fprintf(stderr, "Option::%s called on None\n", op);
    abort();
}

static void panic_err(const char* op) {
    fprintf(stderr, "Result::%s called on Err\n", op);
    abort();
}

static Result result_clone(const Result* r) {
    Result out;
    out.status = r->status;
    out.value = r->value;
    out.err_code = r->err_code;
    out.err_msg = r->err_msg.ptr ? string_copy(r->err_msg) : STRING_EMPTY;
    return out;
}

/* ---- Option constructors ---- */

Option option_none(void) {
    Option opt;
    opt.is_some = 0;
    opt.value.as_i64 = 0;
    return opt;
}

Option option_some_i64(i64 val) {
    Option opt;
    opt.is_some = 1;
    opt.value.as_i64 = val;
    return opt;
}

Option option_some_f64(f64 val) {
    Option opt;
    opt.is_some = 1;
    opt.value.as_f64 = val;
    return opt;
}

Option option_some_ptr(void* val) {
    Option opt;
    opt.is_some = 1;
    opt.value.as_ptr = val;
    return opt;
}

Option option_some_bool(bool_t val) {
    Option opt;
    opt.is_some = 1;
    opt.value.as_bool = val;
    return opt;
}

/* ---- Option queries ---- */

bool_t option_is_some(const Option* opt) {
    if (!opt) return 0;
    return opt->is_some;
}

bool_t option_is_none(const Option* opt) {
    if (!opt) return 1;
    return !opt->is_some;
}

/* ---- Option unwrap (panics on None) ---- */

i64 option_unwrap_i64(const Option* opt) {
    if (!opt || !opt->is_some) panic_none("unwrap_i64");
    return opt->value.as_i64;
}

f64 option_unwrap_f64(const Option* opt) {
    if (!opt || !opt->is_some) panic_none("unwrap_f64");
    return opt->value.as_f64;
}

void* option_unwrap_ptr(const Option* opt) {
    if (!opt || !opt->is_some) panic_none("unwrap_ptr");
    return opt->value.as_ptr;
}

bool_t option_unwrap_bool(const Option* opt) {
    if (!opt || !opt->is_some) panic_none("unwrap_bool");
    return opt->value.as_bool;
}

/* ---- Option unwrap_or ---- */

i64 option_unwrap_or_i64(const Option* opt, i64 default_val) {
    if (!opt || !opt->is_some) return default_val;
    return opt->value.as_i64;
}

f64 option_unwrap_or_f64(const Option* opt, f64 default_val) {
    if (!opt || !opt->is_some) return default_val;
    return opt->value.as_f64;
}

void* option_unwrap_or_ptr(const Option* opt, void* default_val) {
    if (!opt || !opt->is_some) return default_val;
    return opt->value.as_ptr;
}

bool_t option_unwrap_or_bool(const Option* opt, bool_t default_val) {
    if (!opt || !opt->is_some) return default_val;
    return opt->value.as_bool;
}

/* ---- Option combinators ---- */

bool_t option_is_some_and(const Option* opt, bool_t (*pred)(const Option*)) {
    if (!opt || !opt->is_some) return 0;
    if (!pred) return 0;
    return pred(opt);
}

Option option_map(const Option* opt, Option (*mapper)(const Option*)) {
    if (!opt || !opt->is_some) return option_none();
    if (!mapper) return option_none();
    return mapper(opt);
}

Option option_and_then(const Option* opt, Option (*flat_mapper)(const Option*)) {
    if (!opt || !opt->is_some) return option_none();
    if (!flat_mapper) return option_none();
    return flat_mapper(opt);
}

Option option_or(const Option* opt, Option alternative) {
    if (opt && opt->is_some) return *opt;
    return alternative;
}

Option option_or_else(const Option* opt, Option (*fallback)(void)) {
    if (opt && opt->is_some) return *opt;
    if (!fallback) return option_none();
    return fallback();
}

void option_take(Option* opt, Option* out) {
    if (!opt || !out) return;
    *out = *opt;
    opt->is_some = 0;
    opt->value.as_i64 = 0;
}

/* ---- Result constructors ---- */

Result result_ok_i64(i64 val) {
    Result r;
    r.status = RESULT_OK;
    r.value.ok_i64 = val;
    r.err_code = 0;
    r.err_msg = STRING_EMPTY;
    return r;
}

Result result_ok_f64(f64 val) {
    Result r;
    r.status = RESULT_OK;
    r.value.ok_f64 = val;
    r.err_code = 0;
    r.err_msg = STRING_EMPTY;
    return r;
}

Result result_ok_ptr(void* val) {
    Result r;
    r.status = RESULT_OK;
    r.value.ok_ptr = val;
    r.err_code = 0;
    r.err_msg = STRING_EMPTY;
    return r;
}

Result result_err(int code, const char* msg) {
    Result r;
    r.status = RESULT_ERR;
    r.value.ok_i64 = 0;
    r.err_code = code;
    r.err_msg = msg ? string_create(msg) : STRING_EMPTY;
    return r;
}

/* ---- Result queries ---- */

bool_t result_is_ok(const Result* r) {
    if (!r) return 0;
    return r->status == RESULT_OK;
}

bool_t result_is_err(const Result* r) {
    if (!r) return 0;
    return r->status == RESULT_ERR;
}

/* ---- Result unwrap (panics on Err) ---- */

i64 result_unwrap_ok_i64(const Result* r) {
    if (!r || r->status != RESULT_OK) panic_err("unwrap_ok_i64");
    return r->value.ok_i64;
}

f64 result_unwrap_ok_f64(const Result* r) {
    if (!r || r->status != RESULT_OK) panic_err("unwrap_ok_f64");
    return r->value.ok_f64;
}

void* result_unwrap_ok_ptr(const Result* r) {
    if (!r || r->status != RESULT_OK) panic_err("unwrap_ok_ptr");
    return r->value.ok_ptr;
}

int result_err_code(const Result* r) {
    if (!r) return 0;
    return r->err_code;
}

String result_err_msg(const Result* r) {
    if (!r) return STRING_EMPTY;
    return r->err_msg;
}

/* ---- Result combinators ---- */

Result result_map(const Result* r, Result (*mapper)(const Result*)) {
    if (!r) return result_err(-1, "null result");
    if (r->status != RESULT_OK) return result_clone(r);
    if (!mapper) return result_clone(r);
    return mapper(r);
}

Result result_map_err(const Result* r, Result (*mapper)(const Result*)) {
    if (!r) return result_err(-1, "null result");
    if (r->status == RESULT_OK) return result_clone(r);
    if (!mapper) return result_clone(r);
    return mapper(r);
}

Result result_and_then(const Result* r, Result (*flat_mapper)(const Result*)) {
    if (!r) return result_err(-1, "null result");
    if (r->status != RESULT_OK) return result_clone(r);
    if (!flat_mapper) return result_clone(r);
    return flat_mapper(r);
}

/* ---- Conversions ---- */

Option result_to_option(const Result* r) {
    Option opt;
    if (!r || r->status != RESULT_OK) {
        return option_none();
    }
    opt.is_some = 1;
    opt.value.as_i64 = r->value.ok_i64;
    return opt;
}

Result option_to_result(const Option* opt, int err_code, const char* err_msg) {
    Result r;
    if (!opt || !opt->is_some) {
        return result_err(err_code, err_msg);
    }
    r.status = RESULT_OK;
    r.value.ok_i64 = opt->value.as_i64;
    r.err_code = 0;
    r.err_msg = STRING_EMPTY;
    return r;
}

/* ---- Destruction ---- */

void result_destroy(Result* r) {
    if (!r) return;
    if (r->err_msg.ptr) {
        string_free(r->err_msg);
        r->err_msg = STRING_EMPTY;
    }
    r->err_code = 0;
}

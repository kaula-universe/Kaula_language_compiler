#include "matrix.h"
#include "../memory/memory.h"
#include <string.h>

Matrix* matrix_create(size_t rows, size_t cols) {
    if (rows == 0 || cols == 0) return NULL;
    Matrix* m = (Matrix*)kmm_v4_malloc(sizeof(Matrix));
    if (!m) return NULL;
    m->rows = rows;
    m->cols = cols;
    m->data = (Rational*)kmm_v4_calloc(rows * cols, sizeof(Rational));
    if (!m->data) {
        kmm_v4_free(m);
        return NULL;
    }
    for (size_t i = 0; i < rows * cols; i++) {
        m->data[i] = rational_zero();
    }
    return m;
}

void matrix_destroy(Matrix* m) {
    if (!m) return;
    if (m->data) {
        for (size_t i = 0; i < m->rows * m->cols; i++) {
            rational_destroy(&m->data[i]);
        }
        kmm_v4_free(m->data);
        m->data = NULL;
    }
    m->rows = 0;
    m->cols = 0;
    kmm_v4_free(m);
}

Matrix* matrix_copy(const Matrix* src) {
    Matrix* m = matrix_create(src->rows, src->cols);
    if (!m) return NULL;
    for (size_t i = 0; i < src->rows * src->cols; i++) {
        rational_destroy(&m->data[i]);
        m->data[i] = rational_copy(&src->data[i]);
    }
    return m;
}

Matrix* matrix_identity(size_t n) {
    Matrix* m = matrix_create(n, n);
    if (!m) return NULL;
    Rational one = rational_one();
    for (size_t i = 0; i < n; i++) {
        rational_destroy(&m->data[i * n + i]);
        m->data[i * n + i] = rational_copy(&one);
    }
    rational_destroy(&one);
    return m;
}

Matrix* matrix_zero(size_t rows, size_t cols) {
    return matrix_create(rows, cols);
}

size_t matrix_rows(const Matrix* m) {
    return m->rows;
}

size_t matrix_cols(const Matrix* m) {
    return m->cols;
}

bool_t matrix_is_square(const Matrix* m) {
    return m->rows == m->cols;
}

void matrix_get(const Matrix* m, size_t row, size_t col, Rational* out) {
    *out = rational_copy(&m->data[row * m->cols + col]);
}

void matrix_set(Matrix* m, size_t row, size_t col, const Rational* v) {
    rational_destroy(&m->data[row * m->cols + col]);
    m->data[row * m->cols + col] = rational_copy(v);
}

Matrix* matrix_add(const Matrix* a, const Matrix* b) {
    if (a->rows != b->rows || a->cols != b->cols) return NULL;
    Matrix* m = matrix_create(a->rows, a->cols);
    if (!m) return NULL;
    for (size_t i = 0; i < a->rows * a->cols; i++) {
        Rational s = rational_add(&a->data[i], &b->data[i]);
        rational_destroy(&m->data[i]);
        m->data[i] = s;
    }
    return m;
}

Matrix* matrix_subtract(const Matrix* a, const Matrix* b) {
    if (a->rows != b->rows || a->cols != b->cols) return NULL;
    Matrix* m = matrix_create(a->rows, a->cols);
    if (!m) return NULL;
    for (size_t i = 0; i < a->rows * a->cols; i++) {
        Rational s = rational_subtract(&a->data[i], &b->data[i]);
        rational_destroy(&m->data[i]);
        m->data[i] = s;
    }
    return m;
}

Matrix* matrix_multiply(const Matrix* a, const Matrix* b) {
    if (a->cols != b->rows) return NULL;
    Matrix* m = matrix_create(a->rows, b->cols);
    if (!m) return NULL;
    for (size_t i = 0; i < a->rows; i++) {
        for (size_t j = 0; j < b->cols; j++) {
            Rational acc = rational_zero();
            for (size_t k = 0; k < a->cols; k++) {
                Rational prod = rational_multiply(&a->data[i * a->cols + k],
                                                  &b->data[k * b->cols + j]);
                Rational sum = rational_add(&acc, &prod);
                rational_destroy(&acc);
                rational_destroy(&prod);
                acc = sum;
            }
            rational_normalize(&acc);
            rational_destroy(&m->data[i * m->cols + j]);
            m->data[i * m->cols + j] = acc;
        }
    }
    return m;
}

Matrix* matrix_scale(const Matrix* a, const Rational* s) {
    Matrix* m = matrix_create(a->rows, a->cols);
    if (!m) return NULL;
    for (size_t i = 0; i < a->rows * a->cols; i++) {
        Rational prod = rational_multiply(&a->data[i], s);
        rational_destroy(&m->data[i]);
        m->data[i] = prod;
    }
    return m;
}

Matrix* matrix_transpose(const Matrix* a) {
    Matrix* m = matrix_create(a->cols, a->rows);
    if (!m) return NULL;
    for (size_t i = 0; i < a->rows; i++) {
        for (size_t j = 0; j < a->cols; j++) {
            Rational v = rational_copy(&a->data[i * a->cols + j]);
            rational_destroy(&m->data[j * m->cols + i]);
            m->data[j * m->cols + i] = v;
        }
    }
    return m;
}

static int matrix_find_pivot(const Matrix* m, size_t col, size_t start) {
    size_t best = start;
    Rational best_abs = rational_abs(&m->data[start * m->cols + col]);
    for (size_t r = start + 1; r < m->rows; r++) {
        Rational ra = rational_abs(&m->data[r * m->cols + col]);
        if (rational_compare(&ra, &best_abs) > 0) {
            rational_destroy(&best_abs);
            best_abs = ra;
            best = r;
        } else {
            rational_destroy(&ra);
        }
    }
    rational_destroy(&best_abs);
    return best;
}

static void matrix_swap_rows(Matrix* m, size_t r1, size_t r2) {
    if (r1 == r2) return;
    for (size_t j = 0; j < m->cols; j++) {
        Rational t = m->data[r1 * m->cols + j];
        m->data[r1 * m->cols + j] = m->data[r2 * m->cols + j];
        m->data[r2 * m->cols + j] = t;
    }
}

Rational matrix_determinant(const Matrix* m) {
    if (!matrix_is_square(m)) return rational_zero();
    size_t n = m->rows;
    Matrix* w = matrix_copy(m);
    if (!w) return rational_zero();
    Rational det = rational_one();
    Rational neg_one = rational_from_i64(-1);
    for (size_t k = 0; k < n; k++) {
        size_t pivot = matrix_find_pivot(w, k, k);
        if (rational_is_zero(&w->data[pivot * n + k])) {
            rational_destroy(&det);
            det = rational_zero();
            break;
        }
        if (pivot != k) {
            matrix_swap_rows(w, pivot, k);
            Rational t = rational_multiply(&det, &neg_one);
            rational_destroy(&det);
            det = t;
        }
        for (size_t r = k + 1; r < n; r++) {
            if (rational_is_zero(&w->data[r * n + k])) continue;
            Rational factor = rational_divide(&w->data[r * n + k],
                                              &w->data[k * n + k]);
            for (size_t c = k; c < n; c++) {
                Rational prod = rational_multiply(&factor, &w->data[k * n + c]);
                Rational sub = rational_subtract(&w->data[r * n + c], &prod);
                rational_destroy(&prod);
                rational_destroy(&w->data[r * n + c]);
                w->data[r * n + c] = sub;
                rational_normalize(&w->data[r * n + c]);
            }
            rational_destroy(&factor);
        }
    }
    if (!rational_is_zero(&det)) {
        for (size_t k = 0; k < n; k++) {
            Rational t = rational_multiply(&det, &w->data[k * n + k]);
            rational_destroy(&det);
            det = t;
        }
        rational_normalize(&det);
    }
    rational_destroy(&neg_one);
    matrix_destroy(w);
    return det;
}

static Matrix* matrix_gauss_jordan(Matrix* aug) {
    size_t n = aug->rows;
    for (size_t k = 0; k < n; k++) {
        size_t pivot = matrix_find_pivot(aug, k, k);
        if (rational_is_zero(&aug->data[pivot * aug->cols + k])) {
            return NULL;
        }
        if (pivot != k) {
            matrix_swap_rows(aug, pivot, k);
        }
        Rational pivot_val = rational_copy(&aug->data[k * aug->cols + k]);
        for (size_t j = 0; j < aug->cols; j++) {
            Rational d = rational_divide(&aug->data[k * aug->cols + j],
                                         &pivot_val);
            rational_destroy(&aug->data[k * aug->cols + j]);
            aug->data[k * aug->cols + j] = d;
        }
        rational_destroy(&pivot_val);
        for (size_t r = 0; r < n; r++) {
            if (r == k) continue;
            if (rational_is_zero(&aug->data[r * aug->cols + k])) continue;
            for (size_t j = 0; j < aug->cols; j++) {
                Rational prod = rational_multiply(&aug->data[r * aug->cols + k],
                                                  &aug->data[k * aug->cols + j]);
                Rational sub = rational_subtract(&aug->data[r * aug->cols + j], &prod);
                rational_destroy(&prod);
                rational_destroy(&aug->data[r * aug->cols + j]);
                aug->data[r * aug->cols + j] = sub;
                rational_normalize(&aug->data[r * aug->cols + j]);
            }
        }
    }
    return aug;
}

Matrix* matrix_inverse(const Matrix* m) {
    if (!matrix_is_square(m)) return NULL;
    size_t n = m->rows;
    Matrix* aug = matrix_create(n, 2 * n);
    if (!aug) return NULL;
    for (size_t i = 0; i < n; i++) {
        for (size_t j = 0; j < n; j++) {
            Rational v = rational_copy(&m->data[i * n + j]);
            rational_destroy(&aug->data[i * 2 * n + j]);
            aug->data[i * 2 * n + j] = v;
        }
        Rational one = rational_one();
        rational_destroy(&aug->data[i * 2 * n + n + i]);
        aug->data[i * 2 * n + n + i] = one;
    }
    Matrix* solved = matrix_gauss_jordan(aug);
    if (!solved) {
        matrix_destroy(aug);
        return NULL;
    }
    Matrix* inv = matrix_create(n, n);
    if (!inv) {
        matrix_destroy(aug);
        return NULL;
    }
    for (size_t i = 0; i < n; i++) {
        for (size_t j = 0; j < n; j++) {
            Rational v = rational_copy(&aug->data[i * 2 * n + n + j]);
            rational_destroy(&inv->data[i * n + j]);
            inv->data[i * n + j] = v;
        }
    }
    matrix_destroy(aug);
    return inv;
}

Matrix* matrix_solve(const Matrix* a, const Matrix* b) {
    if (!matrix_is_square(a) || b->rows != a->rows || b->cols != 1) return NULL;
    size_t n = a->rows;
    Matrix* aug = matrix_create(n, n + 1);
    if (!aug) return NULL;
    for (size_t i = 0; i < n; i++) {
        for (size_t j = 0; j < n; j++) {
            Rational v = rational_copy(&a->data[i * n + j]);
            rational_destroy(&aug->data[i * (n + 1) + j]);
            aug->data[i * (n + 1) + j] = v;
        }
        Rational v = rational_copy(&b->data[i]);
        rational_destroy(&aug->data[i * (n + 1) + n]);
        aug->data[i * (n + 1) + n] = v;
    }
    Matrix* solved = matrix_gauss_jordan(aug);
    if (!solved) {
        matrix_destroy(aug);
        return NULL;
    }
    Matrix* x = matrix_create(n, 1);
    if (!x) {
        matrix_destroy(aug);
        return NULL;
    }
    for (size_t i = 0; i < n; i++) {
        Rational v = rational_copy(&aug->data[i * (n + 1) + n]);
        rational_destroy(&x->data[i]);
        x->data[i] = v;
    }
    matrix_destroy(aug);
    return x;
}

bool_t matrix_equal(const Matrix* a, const Matrix* b) {
    if (a->rows != b->rows || a->cols != b->cols) return false;
    for (size_t i = 0; i < a->rows * a->cols; i++) {
        if (!rational_equal(&a->data[i], &b->data[i])) return false;
    }
    return true;
}

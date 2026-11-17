#ifndef STD_ALGEBRA_MATRIX_H
#define STD_ALGEBRA_MATRIX_H

#include "../base/types.h"
#include "rational.h"

typedef struct Matrix {
    size_t rows;
    size_t cols;
    Rational* data;
} Matrix;

Matrix* matrix_create(size_t rows, size_t cols);
void matrix_destroy(Matrix* m);
Matrix* matrix_copy(const Matrix* m);
Matrix* matrix_identity(size_t n);
Matrix* matrix_zero(size_t rows, size_t cols);

size_t matrix_rows(const Matrix* m);
size_t matrix_cols(const Matrix* m);
bool_t matrix_is_square(const Matrix* m);

void matrix_get(const Matrix* m, size_t row, size_t col, Rational* out);
void matrix_set(Matrix* m, size_t row, size_t col, const Rational* v);

Matrix* matrix_add(const Matrix* a, const Matrix* b);
Matrix* matrix_subtract(const Matrix* a, const Matrix* b);
Matrix* matrix_multiply(const Matrix* a, const Matrix* b);
Matrix* matrix_scale(const Matrix* a, const Rational* s);
Matrix* matrix_transpose(const Matrix* a);

Rational matrix_determinant(const Matrix* m);
Matrix* matrix_inverse(const Matrix* m);
Matrix* matrix_solve(const Matrix* a, const Matrix* b);

bool_t matrix_equal(const Matrix* a, const Matrix* b);

#endif // STD_ALGEBRA_MATRIX_H
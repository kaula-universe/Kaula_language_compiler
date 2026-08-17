#include "algebra.h"
#include "../math/math.h"
#include <math.h>
#include <string.h>

static const f64 MATRIX_EPSILON = 1e-15;

Vec2 vec2_create(f64 x, f64 y) {
    Vec2 v;
    v.x = x;
    v.y = y;
    return v;
}

Vec2 vec2_zero(void) {
    return vec2_create(0.0, 0.0);
}

Vec2 vec2_add(const Vec2* a, const Vec2* b) {
    return vec2_create(a->x + b->x, a->y + b->y);
}

Vec2 vec2_subtract(const Vec2* a, const Vec2* b) {
    return vec2_create(a->x - b->x, a->y - b->y);
}

Vec2 vec2_scale(const Vec2* a, f64 s) {
    return vec2_create(a->x * s, a->y * s);
}

Vec2 vec2_negate(const Vec2* a) {
    return vec2_create(-a->x, -a->y);
}

Vec2 vec2_normalize(const Vec2* a) {
    f64 len = vec2_length(a);
    if (len == 0.0) return vec2_zero();
    return vec2_create(a->x / len, a->y / len);
}

Vec2 vec2_lerp(const Vec2* a, const Vec2* b, f64 t) {
    return vec2_create(a->x + (b->x - a->x) * t, a->y + (b->y - a->y) * t);
}

f64 vec2_dot(const Vec2* a, const Vec2* b) {
    return a->x * b->x + a->y * b->y;
}

f64 vec2_length(const Vec2* a) {
    return sqrt(vec2_length_squared(a));
}

f64 vec2_length_squared(const Vec2* a) {
    return a->x * a->x + a->y * a->y;
}

f64 vec2_distance(const Vec2* a, const Vec2* b) {
    Vec2 d = vec2_subtract(a, b);
    return vec2_length(&d);
}

bool_t vec2_equal(const Vec2* a, const Vec2* b) {
    return a->x == b->x && a->y == b->y;
}

bool_t vec2_is_zero(const Vec2* a) {
    return a->x == 0.0 && a->y == 0.0;
}

Vec3 vec3_create(f64 x, f64 y, f64 z) {
    Vec3 v;
    v.x = x;
    v.y = y;
    v.z = z;
    return v;
}

Vec3 vec3_zero(void) {
    return vec3_create(0.0, 0.0, 0.0);
}

Vec3 vec3_add(const Vec3* a, const Vec3* b) {
    return vec3_create(a->x + b->x, a->y + b->y, a->z + b->z);
}

Vec3 vec3_subtract(const Vec3* a, const Vec3* b) {
    return vec3_create(a->x - b->x, a->y - b->y, a->z - b->z);
}

Vec3 vec3_scale(const Vec3* a, f64 s) {
    return vec3_create(a->x * s, a->y * s, a->z * s);
}

Vec3 vec3_negate(const Vec3* a) {
    return vec3_create(-a->x, -a->y, -a->z);
}

Vec3 vec3_cross(const Vec3* a, const Vec3* b) {
    return vec3_create(
        a->y * b->z - a->z * b->y,
        a->z * b->x - a->x * b->z,
        a->x * b->y - a->y * b->x
    );
}

Vec3 vec3_normalize(const Vec3* a) {
    f64 len = vec3_length(a);
    if (len == 0.0) return vec3_zero();
    return vec3_create(a->x / len, a->y / len, a->z / len);
}

Vec3 vec3_lerp(const Vec3* a, const Vec3* b, f64 t) {
    return vec3_create(
        a->x + (b->x - a->x) * t,
        a->y + (b->y - a->y) * t,
        a->z + (b->z - a->z) * t
    );
}

f64 vec3_dot(const Vec3* a, const Vec3* b) {
    return a->x * b->x + a->y * b->y + a->z * b->z;
}

f64 vec3_length(const Vec3* a) {
    return sqrt(vec3_length_squared(a));
}

f64 vec3_length_squared(const Vec3* a) {
    return a->x * a->x + a->y * a->y + a->z * a->z;
}

f64 vec3_distance(const Vec3* a, const Vec3* b) {
    Vec3 d = vec3_subtract(a, b);
    return vec3_length(&d);
}

bool_t vec3_equal(const Vec3* a, const Vec3* b) {
    return a->x == b->x && a->y == b->y && a->z == b->z;
}

bool_t vec3_is_zero(const Vec3* a) {
    return a->x == 0.0 && a->y == 0.0 && a->z == 0.0;
}

Vec4 vec4_create(f64 x, f64 y, f64 z, f64 w) {
    Vec4 v;
    v.x = x;
    v.y = y;
    v.z = z;
    v.w = w;
    return v;
}

Vec4 vec4_zero(void) {
    return vec4_create(0.0, 0.0, 0.0, 0.0);
}

Vec4 vec4_add(const Vec4* a, const Vec4* b) {
    return vec4_create(a->x + b->x, a->y + b->y, a->z + b->z, a->w + b->w);
}

Vec4 vec4_subtract(const Vec4* a, const Vec4* b) {
    return vec4_create(a->x - b->x, a->y - b->y, a->z - b->z, a->w - b->w);
}

Vec4 vec4_scale(const Vec4* a, f64 s) {
    return vec4_create(a->x * s, a->y * s, a->z * s, a->w * s);
}

Vec4 vec4_negate(const Vec4* a) {
    return vec4_create(-a->x, -a->y, -a->z, -a->w);
}

Vec4 vec4_normalize(const Vec4* a) {
    f64 len = vec4_length(a);
    if (len == 0.0) return vec4_zero();
    return vec4_create(a->x / len, a->y / len, a->z / len, a->w / len);
}

Vec4 vec4_lerp(const Vec4* a, const Vec4* b, f64 t) {
    return vec4_create(
        a->x + (b->x - a->x) * t,
        a->y + (b->y - a->y) * t,
        a->z + (b->z - a->z) * t,
        a->w + (b->w - a->w) * t
    );
}

f64 vec4_dot(const Vec4* a, const Vec4* b) {
    return a->x * b->x + a->y * b->y + a->z * b->z + a->w * b->w;
}

f64 vec4_length(const Vec4* a) {
    return sqrt(vec4_length_squared(a));
}

f64 vec4_length_squared(const Vec4* a) {
    return a->x * a->x + a->y * a->y + a->z * a->z + a->w * a->w;
}

f64 vec4_distance(const Vec4* a, const Vec4* b) {
    Vec4 d = vec4_subtract(a, b);
    return vec4_length(&d);
}

bool_t vec4_equal(const Vec4* a, const Vec4* b) {
    return a->x == b->x && a->y == b->y && a->z == b->z && a->w == b->w;
}

bool_t vec4_is_zero(const Vec4* a) {
    return a->x == 0.0 && a->y == 0.0 && a->z == 0.0 && a->w == 0.0;
}

Mat2 mat2_create(const f64 data[4]) {
    Mat2 m;
    memcpy(m.m, data, sizeof(m.m));
    return m;
}

Mat2 mat2_zero(void) {
    Mat2 m;
    memset(m.m, 0, sizeof(m.m));
    return m;
}

Mat2 mat2_identity(void) {
    Mat2 m = mat2_zero();
    m.m[0][0] = 1.0;
    m.m[1][1] = 1.0;
    return m;
}

Mat2 mat2_add(const Mat2* a, const Mat2* b) {
    Mat2 r;
    for (size_t i = 0; i < 2; i++) {
        for (size_t j = 0; j < 2; j++) {
            r.m[i][j] = a->m[i][j] + b->m[i][j];
        }
    }
    return r;
}

Mat2 mat2_subtract(const Mat2* a, const Mat2* b) {
    Mat2 r;
    for (size_t i = 0; i < 2; i++) {
        for (size_t j = 0; j < 2; j++) {
            r.m[i][j] = a->m[i][j] - b->m[i][j];
        }
    }
    return r;
}

Mat2 mat2_multiply(const Mat2* a, const Mat2* b) {
    Mat2 r;
    for (size_t i = 0; i < 2; i++) {
        for (size_t j = 0; j < 2; j++) {
            f64 sum = 0.0;
            for (size_t k = 0; k < 2; k++) {
                sum += a->m[i][k] * b->m[k][j];
            }
            r.m[i][j] = sum;
        }
    }
    return r;
}

Mat2 mat2_scale(const Mat2* a, f64 s) {
    Mat2 r;
    for (size_t i = 0; i < 2; i++) {
        for (size_t j = 0; j < 2; j++) {
            r.m[i][j] = a->m[i][j] * s;
        }
    }
    return r;
}

Mat2 mat2_transpose(const Mat2* a) {
    Mat2 r;
    for (size_t i = 0; i < 2; i++) {
        for (size_t j = 0; j < 2; j++) {
            r.m[i][j] = a->m[j][i];
        }
    }
    return r;
}

f64 mat2_determinant(const Mat2* a) {
    return a->m[0][0] * a->m[1][1] - a->m[0][1] * a->m[1][0];
}

Mat2 mat2_inverse(const Mat2* a) {
    f64 det = mat2_determinant(a);
    if (fabs(det) < MATRIX_EPSILON) return mat2_zero();
    Mat2 r;
    r.m[0][0] = a->m[1][1] / det;
    r.m[0][1] = -a->m[0][1] / det;
    r.m[1][0] = -a->m[1][0] / det;
    r.m[1][1] = a->m[0][0] / det;
    return r;
}

Vec2 mat2_mul_vec2(const Mat2* a, const Vec2* v) {
    return vec2_create(
        a->m[0][0] * v->x + a->m[0][1] * v->y,
        a->m[1][0] * v->x + a->m[1][1] * v->y
    );
}

bool_t mat2_equal(const Mat2* a, const Mat2* b) {
    return memcmp(a->m, b->m, sizeof(a->m)) == 0;
}

Mat3 mat3_create(const f64 data[9]) {
    Mat3 m;
    memcpy(m.m, data, sizeof(m.m));
    return m;
}

Mat3 mat3_zero(void) {
    Mat3 m;
    memset(m.m, 0, sizeof(m.m));
    return m;
}

Mat3 mat3_identity(void) {
    Mat3 m = mat3_zero();
    m.m[0][0] = 1.0;
    m.m[1][1] = 1.0;
    m.m[2][2] = 1.0;
    return m;
}

Mat3 mat3_add(const Mat3* a, const Mat3* b) {
    Mat3 r;
    for (size_t i = 0; i < 3; i++) {
        for (size_t j = 0; j < 3; j++) {
            r.m[i][j] = a->m[i][j] + b->m[i][j];
        }
    }
    return r;
}

Mat3 mat3_subtract(const Mat3* a, const Mat3* b) {
    Mat3 r;
    for (size_t i = 0; i < 3; i++) {
        for (size_t j = 0; j < 3; j++) {
            r.m[i][j] = a->m[i][j] - b->m[i][j];
        }
    }
    return r;
}

Mat3 mat3_multiply(const Mat3* a, const Mat3* b) {
    Mat3 r;
    for (size_t i = 0; i < 3; i++) {
        for (size_t j = 0; j < 3; j++) {
            f64 sum = 0.0;
            for (size_t k = 0; k < 3; k++) {
                sum += a->m[i][k] * b->m[k][j];
            }
            r.m[i][j] = sum;
        }
    }
    return r;
}

Mat3 mat3_scale(const Mat3* a, f64 s) {
    Mat3 r;
    for (size_t i = 0; i < 3; i++) {
        for (size_t j = 0; j < 3; j++) {
            r.m[i][j] = a->m[i][j] * s;
        }
    }
    return r;
}

Mat3 mat3_transpose(const Mat3* a) {
    Mat3 r;
    for (size_t i = 0; i < 3; i++) {
        for (size_t j = 0; j < 3; j++) {
            r.m[i][j] = a->m[j][i];
        }
    }
    return r;
}

f64 mat3_determinant(const Mat3* a) {
    const f64* m = (const f64*)a->m;
    return m[0] * (m[4] * m[8] - m[5] * m[7])
         - m[1] * (m[3] * m[8] - m[5] * m[6])
         + m[2] * (m[3] * m[7] - m[4] * m[6]);
}

Mat3 mat3_inverse(const Mat3* a) {
    f64 det = mat3_determinant(a);
    if (fabs(det) < MATRIX_EPSILON) return mat3_zero();
    const f64* m = (const f64*)a->m;
    f64 inv_det = 1.0 / det;
    Mat3 r;
    r.m[0][0] = (m[4] * m[8] - m[5] * m[7]) * inv_det;
    r.m[0][1] = (m[2] * m[7] - m[1] * m[8]) * inv_det;
    r.m[0][2] = (m[1] * m[5] - m[2] * m[4]) * inv_det;
    r.m[1][0] = (m[5] * m[6] - m[3] * m[8]) * inv_det;
    r.m[1][1] = (m[0] * m[8] - m[2] * m[6]) * inv_det;
    r.m[1][2] = (m[2] * m[3] - m[0] * m[5]) * inv_det;
    r.m[2][0] = (m[3] * m[7] - m[4] * m[6]) * inv_det;
    r.m[2][1] = (m[1] * m[6] - m[0] * m[7]) * inv_det;
    r.m[2][2] = (m[0] * m[4] - m[1] * m[3]) * inv_det;
    return r;
}

Vec3 mat3_mul_vec3(const Mat3* a, const Vec3* v) {
    return vec3_create(
        a->m[0][0] * v->x + a->m[0][1] * v->y + a->m[0][2] * v->z,
        a->m[1][0] * v->x + a->m[1][1] * v->y + a->m[1][2] * v->z,
        a->m[2][0] * v->x + a->m[2][1] * v->y + a->m[2][2] * v->z
    );
}

bool_t mat3_equal(const Mat3* a, const Mat3* b) {
    return memcmp(a->m, b->m, sizeof(a->m)) == 0;
}

Mat4 mat4_create(const f64 data[16]) {
    Mat4 m;
    memcpy(m.m, data, sizeof(m.m));
    return m;
}

Mat4 mat4_zero(void) {
    Mat4 m;
    memset(m.m, 0, sizeof(m.m));
    return m;
}

Mat4 mat4_identity(void) {
    Mat4 m = mat4_zero();
    m.m[0][0] = 1.0;
    m.m[1][1] = 1.0;
    m.m[2][2] = 1.0;
    m.m[3][3] = 1.0;
    return m;
}

Mat4 mat4_add(const Mat4* a, const Mat4* b) {
    Mat4 r;
    for (size_t i = 0; i < 4; i++) {
        for (size_t j = 0; j < 4; j++) {
            r.m[i][j] = a->m[i][j] + b->m[i][j];
        }
    }
    return r;
}

Mat4 mat4_subtract(const Mat4* a, const Mat4* b) {
    Mat4 r;
    for (size_t i = 0; i < 4; i++) {
        for (size_t j = 0; j < 4; j++) {
            r.m[i][j] = a->m[i][j] - b->m[i][j];
        }
    }
    return r;
}

Mat4 mat4_multiply(const Mat4* a, const Mat4* b) {
    Mat4 r;
    for (size_t i = 0; i < 4; i++) {
        for (size_t j = 0; j < 4; j++) {
            f64 sum = 0.0;
            for (size_t k = 0; k < 4; k++) {
                sum += a->m[i][k] * b->m[k][j];
            }
            r.m[i][j] = sum;
        }
    }
    return r;
}

Mat4 mat4_scale(const Mat4* a, f64 s) {
    Mat4 r;
    for (size_t i = 0; i < 4; i++) {
        for (size_t j = 0; j < 4; j++) {
            r.m[i][j] = a->m[i][j] * s;
        }
    }
    return r;
}

Mat4 mat4_transpose(const Mat4* a) {
    Mat4 r;
    for (size_t i = 0; i < 4; i++) {
        for (size_t j = 0; j < 4; j++) {
            r.m[i][j] = a->m[j][i];
        }
    }
    return r;
}

f64 mat4_determinant(const Mat4* a) {
    f64 m[4][4];
    memcpy(m, a->m, sizeof(m));
    f64 det = 1.0;
    for (size_t col = 0; col < 4; col++) {
        size_t pivot = col;
        for (size_t r = col + 1; r < 4; r++) {
            if (fabs(m[r][col]) > fabs(m[pivot][col])) pivot = r;
        }
        if (fabs(m[pivot][col]) < MATRIX_EPSILON) return 0.0;
        if (pivot != col) {
            for (size_t c = 0; c < 4; c++) {
                f64 tmp = m[col][c];
                m[col][c] = m[pivot][c];
                m[pivot][c] = tmp;
            }
            det = -det;
        }
        f64 d = m[col][col];
        det *= d;
        for (size_t r = col + 1; r < 4; r++) {
            f64 factor = m[r][col] / d;
            for (size_t c = col; c < 4; c++) {
                m[r][c] -= factor * m[col][c];
            }
        }
    }
    return det;
}

Mat4 mat4_inverse(const Mat4* a) {
    Mat4 inv = mat4_identity();
    Mat4 work = *a;
    for (size_t col = 0; col < 4; col++) {
        size_t pivot = col;
        for (size_t r = col + 1; r < 4; r++) {
            if (fabs(work.m[r][col]) > fabs(work.m[pivot][col])) pivot = r;
        }
        if (fabs(work.m[pivot][col]) < MATRIX_EPSILON) return mat4_zero();
        if (pivot != col) {
            for (size_t c = 0; c < 4; c++) {
                f64 tmp = work.m[col][c];
                work.m[col][c] = work.m[pivot][c];
                work.m[pivot][c] = tmp;
                tmp = inv.m[col][c];
                inv.m[col][c] = inv.m[pivot][c];
                inv.m[pivot][c] = tmp;
            }
        }
        f64 d = work.m[col][col];
        for (size_t c = 0; c < 4; c++) {
            work.m[col][c] /= d;
            inv.m[col][c] /= d;
        }
        for (size_t r = 0; r < 4; r++) {
            if (r == col) continue;
            f64 factor = work.m[r][col];
            for (size_t c = 0; c < 4; c++) {
                work.m[r][c] -= factor * work.m[col][c];
                inv.m[r][c] -= factor * inv.m[col][c];
            }
        }
    }
    return inv;
}

Vec4 mat4_mul_vec4(const Mat4* a, const Vec4* v) {
    return vec4_create(
        a->m[0][0] * v->x + a->m[0][1] * v->y + a->m[0][2] * v->z + a->m[0][3] * v->w,
        a->m[1][0] * v->x + a->m[1][1] * v->y + a->m[1][2] * v->z + a->m[1][3] * v->w,
        a->m[2][0] * v->x + a->m[2][1] * v->y + a->m[2][2] * v->z + a->m[2][3] * v->w,
        a->m[3][0] * v->x + a->m[3][1] * v->y + a->m[3][2] * v->z + a->m[3][3] * v->w
    );
}

bool_t mat4_equal(const Mat4* a, const Mat4* b) {
    return memcmp(a->m, b->m, sizeof(a->m)) == 0;
}

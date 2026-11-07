#ifndef STD_ALGEBRA_ALGEBRA_H
#define STD_ALGEBRA_ALGEBRA_H

#include "../base/types.h"
#include "bigint.h"
#include "rational.h"
#include "poly.h"
#include "matrix.h"

typedef struct Vec2 {
    f64 x;
    f64 y;
} Vec2;

typedef struct Vec3 {
    f64 x;
    f64 y;
    f64 z;
} Vec3;

typedef struct Vec4 {
    f64 x;
    f64 y;
    f64 z;
    f64 w;
} Vec4;

typedef struct Mat2 {
    f64 m[2][2];
} Mat2;

typedef struct Mat3 {
    f64 m[3][3];
} Mat3;

typedef struct Mat4 {
    f64 m[4][4];
} Mat4;

// Vec2
Vec2 vec2_create(f64 x, f64 y);
Vec2 vec2_zero(void);
Vec2 vec2_add(const Vec2* a, const Vec2* b);
Vec2 vec2_subtract(const Vec2* a, const Vec2* b);
Vec2 vec2_scale(const Vec2* a, f64 s);
Vec2 vec2_negate(const Vec2* a);
Vec2 vec2_normalize(const Vec2* a);
Vec2 vec2_lerp(const Vec2* a, const Vec2* b, f64 t);
f64 vec2_dot(const Vec2* a, const Vec2* b);
f64 vec2_length(const Vec2* a);
f64 vec2_length_squared(const Vec2* a);
f64 vec2_distance(const Vec2* a, const Vec2* b);
bool_t vec2_equal(const Vec2* a, const Vec2* b);
bool_t vec2_is_zero(const Vec2* a);

// Vec3
Vec3 vec3_create(f64 x, f64 y, f64 z);
Vec3 vec3_zero(void);
Vec3 vec3_add(const Vec3* a, const Vec3* b);
Vec3 vec3_subtract(const Vec3* a, const Vec3* b);
Vec3 vec3_scale(const Vec3* a, f64 s);
Vec3 vec3_negate(const Vec3* a);
Vec3 vec3_cross(const Vec3* a, const Vec3* b);
Vec3 vec3_normalize(const Vec3* a);
Vec3 vec3_lerp(const Vec3* a, const Vec3* b, f64 t);
f64 vec3_dot(const Vec3* a, const Vec3* b);
f64 vec3_length(const Vec3* a);
f64 vec3_length_squared(const Vec3* a);
f64 vec3_distance(const Vec3* a, const Vec3* b);
bool_t vec3_equal(const Vec3* a, const Vec3* b);
bool_t vec3_is_zero(const Vec3* a);

// Vec4
Vec4 vec4_create(f64 x, f64 y, f64 z, f64 w);
Vec4 vec4_zero(void);
Vec4 vec4_add(const Vec4* a, const Vec4* b);
Vec4 vec4_subtract(const Vec4* a, const Vec4* b);
Vec4 vec4_scale(const Vec4* a, f64 s);
Vec4 vec4_negate(const Vec4* a);
Vec4 vec4_normalize(const Vec4* a);
Vec4 vec4_lerp(const Vec4* a, const Vec4* b, f64 t);
f64 vec4_dot(const Vec4* a, const Vec4* b);
f64 vec4_length(const Vec4* a);
f64 vec4_length_squared(const Vec4* a);
f64 vec4_distance(const Vec4* a, const Vec4* b);
bool_t vec4_equal(const Vec4* a, const Vec4* b);
bool_t vec4_is_zero(const Vec4* a);

// Mat2
Mat2 mat2_create(const f64 data[4]);
Mat2 mat2_zero(void);
Mat2 mat2_identity(void);
Mat2 mat2_add(const Mat2* a, const Mat2* b);
Mat2 mat2_subtract(const Mat2* a, const Mat2* b);
Mat2 mat2_multiply(const Mat2* a, const Mat2* b);
Mat2 mat2_scale(const Mat2* a, f64 s);
Mat2 mat2_transpose(const Mat2* a);
Mat2 mat2_inverse(const Mat2* a);
Vec2 mat2_mul_vec2(const Mat2* a, const Vec2* v);
f64 mat2_determinant(const Mat2* a);
bool_t mat2_equal(const Mat2* a, const Mat2* b);

// Mat3
Mat3 mat3_create(const f64 data[9]);
Mat3 mat3_zero(void);
Mat3 mat3_identity(void);
Mat3 mat3_add(const Mat3* a, const Mat3* b);
Mat3 mat3_subtract(const Mat3* a, const Mat3* b);
Mat3 mat3_multiply(const Mat3* a, const Mat3* b);
Mat3 mat3_scale(const Mat3* a, f64 s);
Mat3 mat3_transpose(const Mat3* a);
Mat3 mat3_inverse(const Mat3* a);
Vec3 mat3_mul_vec3(const Mat3* a, const Vec3* v);
f64 mat3_determinant(const Mat3* a);
bool_t mat3_equal(const Mat3* a, const Mat3* b);

// Mat4
Mat4 mat4_create(const f64 data[16]);
Mat4 mat4_zero(void);
Mat4 mat4_identity(void);
Mat4 mat4_add(const Mat4* a, const Mat4* b);
Mat4 mat4_subtract(const Mat4* a, const Mat4* b);
Mat4 mat4_multiply(const Mat4* a, const Mat4* b);
Mat4 mat4_scale(const Mat4* a, f64 s);
Mat4 mat4_transpose(const Mat4* a);
Mat4 mat4_inverse(const Mat4* a);
Vec4 mat4_mul_vec4(const Mat4* a, const Vec4* v);
f64 mat4_determinant(const Mat4* a);
bool_t mat4_equal(const Mat4* a, const Mat4* b);

#endif // STD_ALGEBRA_ALGEBRA_H
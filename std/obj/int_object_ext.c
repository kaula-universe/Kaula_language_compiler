#include "int_object.h"
#include "int_object_ext.h"
#include <stdbool.h>
#include <stddef.h>

// 整数比较函数
bool int_object_less(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return false;
    return self->value < other->value;
}

bool int_object_greater(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return false;
    return self->value > other->value;
}

bool int_object_less_equal(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return false;
    return self->value <= other->value;
}

bool int_object_greater_equal(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return false;
    return self->value >= other->value;
}

// 整数模运算
IntObject* int_object_mod(IntObject* self, IntObject* other) {
    if (self == NULL || other == NULL) return NULL;
    if (other->value == 0) return NULL;  // 除零错误
    return int_object_create(self->value % other->value);
}

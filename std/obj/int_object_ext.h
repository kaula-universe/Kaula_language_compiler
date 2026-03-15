// 整数比较函数
extern bool int_object_less(IntObject* self, IntObject* other);
extern bool int_object_greater(IntObject* self, IntObject* other);
extern bool int_object_less_equal(IntObject* self, IntObject* other);
extern bool int_object_greater_equal(IntObject* self, IntObject* other);

// 整数模运算
extern IntObject* int_object_mod(IntObject* self, IntObject* other);

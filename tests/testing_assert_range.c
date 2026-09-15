#include "../std/testing/testing.h"
#include <assert.h>

int main(void) {
    String message = string_create("range");
    String file = string_create("testing_assert_range.c");
    assert(test_assert_int_range(5, 1, 10, message, file, 1));
    assert(!test_assert_int_range(0, 1, 10, message, file, 2));
    return 0;
}

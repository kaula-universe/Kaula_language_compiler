// BENCH:PREFIX
#include <math.h>
#include <stdbool.h>
#include <stdio.h>

// BENCH:BEGIN
bool has_close_elements(const double* numbers, int n, double threshold) {
    for (int i = 0; i < n; i++) {
        for (int j = 0; j < n; j++) {
            if (i != j && fabs(numbers[i] - numbers[j]) < threshold) {
                return true;
            }
        }
    }
    return false;
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main(void) {
    double a[6] = {1.0, 2.0, 3.9, 4.0, 5.0, 2.2};
    if (!has_close_elements(a, 6, 0.3)) return 1;
    if (has_close_elements(a, 6, 0.05)) return 2;
    printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

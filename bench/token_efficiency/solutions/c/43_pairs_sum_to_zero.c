// BENCH:BEGIN
#include <stdbool.h>
#include <stdio.h>

bool pairs_sum_to_zero(const long* l, int n) {
    for (int i = 0; i < n; i++) {
        for (int j = i + 1; j < n; j++) {
            if (l[i] + l[j] == 0) {
                return true;
            }
        }
    }
    return false;
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main(void) {
    long a[6] = {2, 4, -5, 3, 5, 7};
    long b[4] = {1, 3, 5, 0};
    if (pairs_sum_to_zero(b, 4)) return 1;
    if (!pairs_sum_to_zero(a, 6)) return 2;
    printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

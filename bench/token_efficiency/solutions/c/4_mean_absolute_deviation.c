// BENCH:PREFIX
#include <math.h>
#include <stdio.h>

// BENCH:BEGIN
double mean_absolute_deviation(const double* numbers, int n) {
    double sum = 0.0;
    for (int i = 0; i < n; i++) {
        sum += numbers[i];
    }
    double mean = sum / (double)n;
    double dev = 0.0;
    for (int i = 0; i < n; i++) {
        dev += fabs(numbers[i] - mean);
    }
    return dev / (double)n;
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main(void) {
    double a[4] = {1.0, 2.0, 3.0, 4.0};
    if (fabs(mean_absolute_deviation(a, 4) - 1.0) > 1e-6) return 1;
    printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

// BENCH:PREFIX
#include <math.h>
#include <stdio.h>

// BENCH:BEGIN
double truncate_number(double number) {
    return fmod(number, 1.0);
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main(void) {
    if (truncate_number(3.5) != 0.5) return 1;
    printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

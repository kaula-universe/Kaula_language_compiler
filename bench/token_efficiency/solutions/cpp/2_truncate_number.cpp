// BENCH:PREFIX
#include <cmath>
#include <cstdio>

// BENCH:BEGIN
double truncate_number(double number) {
    return std::fmod(number, 1.0);
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main() {
    if (truncate_number(3.5) != 0.5) return 1;
    std::printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

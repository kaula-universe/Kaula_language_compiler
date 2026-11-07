// BENCH:BEGIN
#include <cmath>
#include <cstdio>
#include <vector>

bool has_close_elements(const std::vector<double>& numbers, double threshold) {
    for (size_t i = 0; i < numbers.size(); i++) {
        for (size_t j = 0; j < numbers.size(); j++) {
            if (i != j && std::fabs(numbers[i] - numbers[j]) < threshold) {
                return true;
            }
        }
    }
    return false;
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main() {
    std::vector<double> a{1.0, 2.0, 3.9, 4.0, 5.0, 2.2};
    if (!has_close_elements(a, 0.3)) return 1;
    if (has_close_elements(a, 0.05)) return 2;
    std::printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

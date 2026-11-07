// BENCH:BEGIN
#include <cmath>
#include <cstdio>
#include <numeric>
#include <vector>

double mean_absolute_deviation(const std::vector<double>& numbers) {
    double sum = std::accumulate(numbers.begin(), numbers.end(), 0.0);
    double mean = sum / static_cast<double>(numbers.size());
    double dev = 0.0;
    for (double v : numbers) {
        dev += std::fabs(v - mean);
    }
    return dev / static_cast<double>(numbers.size());
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main() {
    std::vector<double> a{1.0, 2.0, 3.0, 4.0};
    if (std::fabs(mean_absolute_deviation(a) - 1.0) > 1e-6) return 1;
    std::printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

// BENCH:PREFIX
#include <cstdio>
#include <vector>

// BENCH:BEGIN
bool pairs_sum_to_zero(const std::vector<int>& l) {
    for (size_t i = 0; i < l.size(); i++) {
        for (size_t j = i + 1; j < l.size(); j++) {
            if (l[i] + l[j] == 0) {
                return true;
            }
        }
    }
    return false;
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main() {
    if (pairs_sum_to_zero({1, 3, 5, 0})) return 1;
    if (!pairs_sum_to_zero({2, 4, -5, 3, 5, 7})) return 2;
    std::printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

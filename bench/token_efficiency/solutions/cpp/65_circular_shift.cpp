// BENCH:PREFIX
#include <algorithm>
#include <cstdio>
#include <string>

// BENCH:BEGIN
std::string circular_shift(int x, int shift) {
    std::string s = std::to_string(x);
    if (shift > static_cast<int>(s.size())) {
        std::reverse(s.begin(), s.end());
        return s;
    }
    return s.substr(s.size() - shift) + s.substr(0, s.size() - shift);
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main() {
    if (circular_shift(100, 2) != "001") return 1;
    if (circular_shift(97, 8) != "79") return 2;
    std::printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

// BENCH:PREFIX
#include <cstdio>
#include <string>

// BENCH:BEGIN
int strlen(const std::string& s) {
    return static_cast<int>(s.size());
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main() {
    std::string a = "abc";
    if (strlen(std::string()) != 0 || strlen(a) != 3) return 1;
    std::printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

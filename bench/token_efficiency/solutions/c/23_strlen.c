// BENCH:BEGIN
#include <stdio.h>
#include <string.h>

int strlen_of(const char* s) {
    return (int)strlen(s);
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main(void) {
    if (strlen_of("") != 0 || strlen_of("abc") != 3) return 1;
    printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

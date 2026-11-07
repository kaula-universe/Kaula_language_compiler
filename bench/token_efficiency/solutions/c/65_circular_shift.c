// BENCH:BEGIN
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

char* circular_shift(long x, long shift) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%ld", x);
    int n = (int)strlen(buf);
    char* out = (char*)malloc((size_t)n + 1);
    if (shift > n) {
        for (int i = 0; i < n; i++) {
            out[i] = buf[n - 1 - i];
        }
    } else {
        for (int i = 0; i < n; i++) {
            out[i] = buf[(n - (int)shift + i) % n];
        }
    }
    out[n] = '\0';
    return out;
}

// BENCH:END

// BENCH:MAIN:BEGIN
int main(void) {
    char* a = circular_shift(100, 2);
    if (strcmp(a, "001") != 0) return 1;
    free(a);
    char* b = circular_shift(97, 8);
    if (strcmp(b, "79") != 0) return 2;
    free(b);
    printf("ok\n");
    return 0;
}
// BENCH:MAIN:END

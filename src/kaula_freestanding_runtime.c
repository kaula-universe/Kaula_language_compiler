// Kaula Freestanding Runtime
// 裸机模式下的最小 C 运行时
// 提供 LLVM 在大小未知时降级调用的内存操作函数
// 这些函数在 hosted 模式下由 libc 提供，freestanding 模式下必须自行实现

#include <stdint.h>
#include <stddef.h>

// memset - 内存设置
// LLVM 在大小未知时会降级为对此符号的调用
void* memset(void* dst, int c, size_t n) {
    unsigned char* d = (unsigned char*)dst;
    while (n--) {
        *d++ = (unsigned char)c;
    }
    return dst;
}

// memcpy - 内存复制（不重叠）
// LLVM 在大小未知时会降级为对此符号的调用
void* memcpy(void* dst, const void* src, size_t n) {
    unsigned char* d = (unsigned char*)dst;
    const unsigned char* s = (const unsigned char*)src;
    while (n--) {
        *d++ = *s++;
    }
    return dst;
}

// memmove - 内存复制（可重叠）
void* memmove(void* dst, const void* src, size_t n) {
    unsigned char* d = (unsigned char*)dst;
    const unsigned char* s = (const unsigned char*)src;
    if (d < s) {
        // 正向复制
        while (n--) {
            *d++ = *s++;
        }
    } else {
        // 反向复制（避免重叠覆盖）
        d += n;
        s += n;
        while (n--) {
            *--d = *--s;
        }
    }
    return dst;
}

// memcmp - 内存比较
int memcmp(const void* a, const void* b, size_t n) {
    const unsigned char* p = (const unsigned char*)a;
    const unsigned char* q = (const unsigned char*)b;
    while (n--) {
        if (*p != *q) {
            return (int)*p - (int)*q;
        }
        p++;
        q++;
    }
    return 0;
}

// strlen - 字符串长度
size_t strlen(const char* s) {
    const char* p = s;
    while (*p) {
        p++;
    }
    return (size_t)(p - s);
}

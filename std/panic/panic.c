#include "panic.h"
#include <stdio.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>

#if defined(_MSC_VER)
#define PANIC_THREAD_LOCAL __declspec(thread)
#else
#define PANIC_THREAD_LOCAL _Thread_local
#endif

#define PANIC_MSG_CAP 512

// 当前 try 帧链（thread-local，支持嵌套 try）
static PANIC_THREAD_LOCAL PanicFrame* g_current = NULL;

// 最近一次 panic 的消息与错误码（thread-local，longjmp 返回后仍有效）
static PANIC_THREAD_LOCAL char g_message[PANIC_MSG_CAP];
static PANIC_THREAD_LOCAL int g_last_code = PANIC_CODE_UNKNOWN;

int panic_try(PanicFrame* frame) {
    if (!frame) {
        return 0;
    }
    // 注意：setjmp 注册在此函数自己的帧上。panic_try 返回、帧销毁后再
    // longjmp 回来属于未定义行为（Windows x64 会 failfast），
    // 因此 panic_try/panic_leave 仅适合 C 宏封装场景；
    // Kaula 侧请优先使用 panic_protect（回调期间帧一直存活）。
    frame->prev = g_current;
    frame->caught = 0;
    g_current = frame;
    if (setjmp(frame->env)) {
        frame->caught = 1;
        return 1;
    }
    return 0;
}

void panic_leave(PanicFrame* frame) {
    if (!frame) {
        return;
    }
    if (g_current == frame) {
        g_current = frame->prev;
    }
}

// 推荐入口：受保护代码包在回调里，setjmp 帧在回调执行期间始终在栈上，
// panic 时 longjmp 回跳合法（C11 保证、Windows x64 帧校验通过）。
int panic_protect(PanicFrame* frame, PanicBody body, void* ctx) {
    if (!frame || !body) {
        return -1;
    }
    frame->prev = g_current;
    frame->caught = 0;
    g_current = frame;
    if (setjmp(frame->env)) {
        frame->caught = 1;
    } else {
        body(ctx);
    }
    g_current = frame->prev;
    return frame->caught;
}

// 抛出 panic：先记录消息与错误码，再决定跳转或终止
static void panic_raise(int code, const char* message) {
    g_last_code = code;
    if (message && message[0] != '\0') {
        snprintf(g_message, sizeof(g_message), "%s", message);
    } else {
        g_message[0] = '\0';
    }
    if (g_current) {
        longjmp(g_current->env, 1);
    }
    fprintf(stderr, "panic: %s\n", g_message);
    abort();
}

void panic(const char* message) {
    panic_raise(PANIC_CODE_UNKNOWN, message ? message : "panic");
}

void panic_with_code(int code, const char* message) {
    panic_raise(code, message ? message : "panic");
}

void panicf(const char* fmt, ...) {
    char buffer[PANIC_MSG_CAP];
    va_list args;
    va_start(args, fmt);
    if (fmt) {
        vsnprintf(buffer, sizeof(buffer), fmt, args);
    } else {
        buffer[0] = '\0';
    }
    va_end(args);
    panic_raise(PANIC_CODE_UNKNOWN, buffer);
}

const char* panic_message(void) {
    return g_message;
}

int panic_last_code(void) {
    return g_last_code;
}

bool panic_active(void) {
    return g_current != NULL;
}
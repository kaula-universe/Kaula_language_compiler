#pragma once
#include "../base/types.h"
#include <setjmp.h>

/**
 * std/panic/panic.h - 可捕获的 panic（Kaula 错误处理基石）
 *
 * 用 setjmp/longjmp 实现 try/catch 语义：
 *
 *   PanicFrame f
 *   if panic_try(&f) == 0 {
 *       risky_call()          # 正常路径
 *       panic_leave(&f)       # 必须调用，恢复上一帧
 *   } else {
 *       println("caught: ", panic_message())
 *       panic_leave(&f)       # 捕获路径同样必须恢复
 *   }
 *
 * 更安全（推荐）的写法是 panic_protect —— 回调体内发生 panic 时，
 * setjmp 帧仍在栈上，跨函数 longjmp 完全合法：
 *
 *   fn risky_body(ctx: void()) { risky_call() }
 *   PanicFrame f
 *   if panic_protect(&f, &risky_body, null) == 1 {
 *       println("caught: ", panic_message())
 *   }
 *
 * panic 在 try 块外调用时行为与 C assert 一致：打印消息到 stderr 并 abort。
 * panic_try/panic_leave（或 panic_protect）必须配对使用；多层 try 自动嵌套
 * （栈式帧链），未捕获的 panic 会沿当前帧向上冒泡到最近的 try。
 */

#define PANIC_CODE_UNKNOWN 0

// panic 帧：jmp_buf 保存点 + 上一层帧指针 + 捕获标志（panic_protect 使用）
typedef struct PanicFrame {
    jmp_buf env;
    struct PanicFrame* prev;
    int caught;
} PanicFrame;

// 受保护代码块的回调（void() 为透明指针＝void*）
typedef void (*PanicBody)(void* ctx);

// 进入 try：首次调用返回 0（正常路径）；捕获到 panic 时返回非 0（catch 路径）
extern int panic_try(PanicFrame* frame);

// 退出 try：恢复上一层帧。正常路径与捕获路径都必须调用一次
extern void panic_leave(PanicFrame* frame);

// 推荐用法：用回调包裹受保护代码。
// setjmp 注册在 panic_protect 自己的帧上，回调期间该帧一直在栈上，
// panic 发生时 longjmp 回跳完全合法（Windows x64 也安全）。
// 返回值：0 = 回调正常完成；1 = 捕获到 panic（panic_message()/panic_last_code() 可用）
extern int panic_protect(PanicFrame* frame, PanicBody body, void* ctx);

// 抛出 panic（不可返回）。在 try 内会跳转到最近的 panic_try；否则打印并 abort
extern void panic(const char* message);

// 抛出带错误码的 panic
extern void panic_with_code(int code, const char* message);

// 格式化抛出 panic（printf 风格）
extern void panicf(const char* fmt, ...);

// 当前捕获到的 panic 消息（仅在 catch 路径有效）
extern const char* panic_message(void);

// 当前捕获到的 panic 错误码
extern int panic_last_code(void);

// 是否处于 try 块内（panic 可被捕获）
extern bool panic_active(void);
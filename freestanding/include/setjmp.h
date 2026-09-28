// freestanding/include/setjmp.h — 裸机模式的 <setjmp.h> 替身
//
// setjmp/longjmp 需要 ABI 相关的上下文布局，跨目标无法用一份 C 定义保证正确
// （x86_64 与 aarch64 的 jmp_buf 布局完全不同）。这里只提供类型与原型：
//   - 能编过：引用 setjmp/longjmp 的代码不再卡在 "file not found"
//   - 链接期未定义：如实反映"裸机异常/非局部跳转需要按目标自行实现"
// 需要真正可用时，在自己的目标上按 ABI 实现这两个符号（jmp_buf 布局见
// 各 ABI 的 setjmp 约定），或改用 Kaula 的 panic_protect 机制。
#ifndef KAULA_FS_COMPAT_SETJMP_H
#define KAULA_FS_COMPAT_SETJMP_H

#ifdef __cplusplus
extern "C" {
#endif

// 容量取各主流 ABI 的最大值：x86_64 glibc 200、aarch64 glibc 200、
// riscv64 400。真实布局由用户实现决定，这里只保证数组够大。
typedef long jmp_buf[64];
typedef long sigjmp_buf[64];

int setjmp(jmp_buf env);
int _setjmp(jmp_buf env);
void longjmp(jmp_buf env, int val);
void _longjmp(jmp_buf env, int val);
int sigsetjmp(sigjmp_buf env, int savemask);
void siglongjmp(sigjmp_buf env, int val);

#define setjmp(env)     setjmp(env)
#define sigsetjmp(e, m) sigsetjmp(e, m)

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_SETJMP_H */

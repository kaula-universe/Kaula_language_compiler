# 入门指南：Hello, Kaula!

Kaula 是一门编译到 C 的系统编程语言，支持现代语言特性（泛型、模式匹配、编译期反射等），
同时保持与 C 生态的完全互操作性。

## 安装

确保满足以下依赖：

- **Go** (编译 kaulac 编译器)
- **LLVM-mingw 或 MSVC** (Windows) / **Clang** (Linux/macOS) (编译生成的 C 代码)

## 第一个程序

创建一个 `hello.kl` 文件：

```kaula
import std.io

fn main() {
    println("Hello, Kaula!")
}
```

## 编译

使用 `kaulac` 编译器处理 Kaula 源码：

```sh
kaulac hello.kl
```

`kaulac` 会生成 C 代码并自动调用 Clang 编译为可执行文件。
生成的 `.c` 文件会缓存到 `cache/` 目录。

## 运行

```sh
./hello
# 输出: Hello, Kaula!
```

## Kaula 语言的核心设计理念

- **编译到 C**：Kaula 源码→C 代码→机器码，与现有 C 库无缝互操作
- **渐进类型**：支持显式类型和 `auto` 类型推导
- **内存安全**：KMM V4 per-thread heap 作用域分配器（默认启用，比 malloc 快 2-20x）+ 可选 SOR 所有权分析
- **零开销抽象**：结构体、枚举（带变体）、泛型等抽象在编译期展开，无运行时开销

## 内存管理

Kaula 默认使用 KMM V4 作为内存分配器，无需手动 free：

```kaula
import std.memory

fn main() {
    // 直接使用 KMM V4 池分配器，作用域退出自动回收
    auto buf = std.memory.kmm_v4_alloc(1024)
    
    // 使用 buf...
    memset(buf, 0, 1024)
    
    // 函数退出时自动回收，无需 free
}
```

KMM V4 基于 per-thread heap + bump allocation，在基准测试中比 malloc/free 快 2-20x（纯分配路径快 20x+，小对象分配快 10x+）。

## 完整的示例文件

参见 [examples/hello.kl](examples/hello.kl)。

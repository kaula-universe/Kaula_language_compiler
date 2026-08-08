# kaulafmt — Kaula 代码格式化工具

`kaulafmt` 格式化 `.kl` 源文件，使代码符合统一风格。保存路径：`build/bin/kaulafmt.exe`（源码：`kaula-compiler/cmd/kaulafmt/`）。

## 用法

```
用法: kaulafmt <input.kl>             # 格式化结果输出到标准输出（预览）
       kaulafmt -w <input.kl>         # 格式化结果写回原文件
```

| 参数 | 说明 |
|------|------|
| `<input.kl>` | 要格式化的 Kaula 源文件（**仅支持单个文件**，必须 `.kl` 扩展名） |
| `-w` | 将格式化结果写回原文件（in-place 编辑） |

```bash
kaulafmt main.kl          # 预览格式化结果
kaulafmt -w main.kl       # 格式化并写回
```

> 注意：`kaulafmt` 一次只能处理一个文件；对多文件可配合 shell 循环：
> ```bash
> for f in src/*.kl; do kaulafmt -w "$f"; done
> ```

## 格式化规则

### 缩进与空行

- 缩进单位：**4 个空格**，不使用 Tab
- 顶层语句之间用空行分隔
- import 块自动排序：`std.*` 优先（内部无空行、字母序），其他 import 之后空一行（字母序）

### 代码风格

| 元素 | 规则 |
|------|------|
| 函数 | `fn name(params) ReturnType { ... }`；空函数体输出为 `fn empty() {}` |
| 参数 | 类型后置：`fn add(a: i32, b: i32) i32` |
| 类/接口 | 字段 `name: Type`，方法/构造函数缩进一级 |
| 变量 | `Type name = value` / `auto name = value`，`const`/`static`/`pub` 前缀保留 |
| import | `import std.io` 裸模块名，不加引号 |
| 注解 | `#[name(args)]` 放在被修饰语句之前 |
| match | `match(target) {`，分支 `pattern => { ... }` |
| spend | `spend target { call index { ... } }` |
| 强转 | 仅保留 `as<T>(expr)` 形式 |
| 位运算/取址 | 一元运算符（`*` `&` `-` `~`）紧贴右操作数，无空格 |
| 二元运算 | `a + b`（运算符两侧各一空格） |

### 输出特性

- 布尔/整数/浮点/字符串/字符字面量规范化输出
- 未知语句/表达式类型输出注释标记：`/* unknown statement: %T */`
- 注释会丢失（不保留）
- 手动换行不保留

## 工作原理

1. 词法分析（`internal/lexer`）→ Token 流
2. 语法分析（`internal/parser`，跳过 main 检查）→ AST
3. 遍历 AST 按规则生成源码

解析失败（含语法错误）时输出 `Error: Failed to parse <file>` 并以非零码退出。

## 构建

```bash
python toolkit_build.py                    # 随工具链整体构建
cd kaula-compiler/cmd/kaulafmt && go build -o kaulafmt.exe   # 单独构建
```

## 限制

- 一次仅一个文件；不支持目录/通配符
- 不支持自定义缩进风格
- 保留注释（注释在格式化过程中丢失）
- 未知语句类型被注释标记，不崩溃
- 注解的自定义格式不支持
- 不支持保留原始代码中的手动换行

## 相关文档

- 工具索引：[README.md](README.md)
- 格式化器实现原理：[代码生成器 (Codegen)](../code-generation.md)
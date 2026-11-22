# 调试支持

Kaula 编译器支持生成 DWARF 调试符号，可配合 GDB/LLDB 进行源码级调试。

## 快速开始

```bash
# 生成调试符号（行号表模式，开销最小）
kaulac --debug main.kl

# 生成完整调试符号（含类型信息）
kaulac --debug --debug-level full main.kl

# 使用 GDB 调试
gdb ./main
(gdb) source /path/to/tools/debug/kaula_pretty_printers.py
(gdb) break main.kl:10
(gdb) run

# 使用 LLDB 调试
lldb ./main
(lldb) command script import /path/to/tools/debug/kaula_lldb_formatters.py
(lldb) breakpoint set --file main.kl --line 10
(lldb) run
```

## 编译选项

| 选项 | 说明 |
|------|------|
| `--debug` | 生成 DWARF 调试符号（`-g`） |
| `--debug-level <level>` | 调试级别：`line-tables`（默认）/ `full` |

### 调试级别

- **line-tables**（默认）：仅生成行号表，体积小，开销最低
- **full**：生成完整 DWARF 信息，包含类型、变量、作用域等，体积较大

```bash
# 轻量级调试（仅行号）
kaulac --debug main.kl

# 完整调试（含类型信息）
kaulac --debug --debug-level full main.kl
```

## 源码映射

Kaula 编译器生成 C 代码后，源码映射文件 `<name>.map.json` 记录了 Kaula 源码行号到生成 C 代码行号的对应关系。

```bash
# 同时生成调试符号和源码映射
kaulac --debug --sourcemap main.kl
```

### 映射格式

```json
{
  "version": 2,
  "source": "main.kl",
  "target": "main.c",
  "entries": [
    {
      "generated_line": 15,
      "source_file": "main.kl",
      "source_line": 10,
      "source_column": 1,
      "kind": "function",
      "symbol_name": "main",
      "kl_type": "i32",
      "c_type_name": "int32_t"
    }
  ],
  "variable_map": {
    "x": "x",
    "count": "count"
  },
  "type_map": {
    "i32": "int32_t",
    "String": "String"
  }
}
```

## GDB Pretty-Printers

为 Kaula 类型提供可读的调试输出。

### 安装

```bash
# 在 .gdbinit 中添加
source /path/to/tools/debug/kaula_pretty_printers.py

# 或在 GDB 会话中
(gdb) source /path/to/tools/debug/kaula_pretty_printers.py
```

### 支持的类型

| 类型 | 输出示例 |
|------|----------|
| `String` | `"hello world"` |
| `Option<T>` | `Some(42)` / `None` |
| `Result<T,E>` | `Ok(42)` / `Err(Error(code=1))` |
| `Error` | `Error(code=1, msg="file not found")` |

## LLDB Formatters

为 LLDB 提供类型摘要和合成器。

### 安装

```bash
# 在 LLDB 会话中
(lldb) command script import /path/to/tools/debug/kaula_lldb_formatters.py

# 或在 ~/.lldbinit 中添加
command script import /path/to/tools/debug/kaula_lldb_formatters.py
```

### 支持的类型

| 类型 | 摘要 |
|------|------|
| `String` | `"hello world"` |
| `Option<T>` | `Some(42)` / `None` |
| `Result<T,E>` | `Ok(42)` / `Err(...)` |
| `Error` | `Error(code=1, msg="...")` |

## 类型映射

Kaula 类型到 C 类型的映射：

| Kaula 类型 | C 类型 |
|------------|--------|
| `i8` / `i16` / `i32` / `i64` | `int8_t` / `int16_t` / `int32_t` / `int64_t` |
| `u8` / `u16` / `u32` / `u64` | `uint8_t` / `uint16_t` / `uint32_t` / `uint64_t` |
| `f32` / `f64` | `float` / `double` |
| `bool` | `int` |
| `usize` | `size_t` |
| `String` | `String` (struct { len, ptr }) |
| `Option<T>` | struct with tag union |
| `Result<T,E>` | struct with tag union |

## 调试工作流

### 1. 编译

```bash
kaulac --debug --sourcemap main.kl
```

### 2. 启动调试器

```bash
# GDB
gdb ./main

# LLDB
lldb ./main
```

### 3. 加载 pretty-printers

```bash
# GDB
(gdb) source tools/debug/kaula_pretty_printers.py

# LLDB
(lldb) command script import tools/debug/kaula_lldb_formatters.py
```

### 4. 设置断点

```bash
# GDB - 按 Kaula 源码行号
(gdb) break main.kl:10

# GDB - 按函数名
(gdb) break main

# LLDB - 按 Kaula 源码行号
(lldb) breakpoint set --file main.kl --line 10

# LLDB - 按函数名
(lldb) breakpoint set --name main
```

### 5. 运行和调试

```bash
(gdb) run
(gdb) print x          # 查看变量
(gdb) backtrace        # 查看调用栈
(gdb) step             # 单步进入
(gdb) next             # 单步跳过
```

## 注意事项

1. **调试符号与优化**：调试模式下建议使用 `-O0`（`--opt O0`），否则优化可能导致行号映射不准确
2. **Windows 支持**：需要安装 LLVM/Clang 和 GDB/LLDB
3. **源码映射**：`--sourcemap` 生成的 `.map.json` 文件是 Kaula 特有的，不等同于 DWARF
4. **Kaula 源码包**：pkglib 中的 Kaula 源码包在 import 时会被编译，调试符号会包含在最终二进制中

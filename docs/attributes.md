# Kaula `#[]` 高级属性扩展

Kaula 支持通过 `#[attribute]` 语法为函数、结构体、字段等添加元数据注解。本文档介绍高级属性扩展：`#[test]`、`#[derive]`、`#[command]`。

## 目录

- [`#[test]` 自动测试](#test-自动测试)
- [`#[derive]` 派生宏](#derive-派生宏)
- [`#[command]` 命令行参数解析](#command-命令行参数解析)
- [属性组合使用](#属性组合使用)

---

## `#[test]` 自动测试

### 概述

`#[test]` 属性标记一个函数为测试函数。使用 `kaulac test` 子命令编译时，编译器会自动收集所有测试函数，生成测试运行器（test runner），并在编译完成后自动执行。

### 基本用法

```kaula
#[test]
fn test_addition() {
    i64 result = 2 + 3
    assert(result == 5)
}
```

断言说明：`assert(cond)` 为编译器内置，无需 import。测试模式下失败会被
runner 记录（测试计为 FAILED，`kaulac test` 返回非零退出码）；普通模式下
失败会打印断言文本并 `exit(1)`。

### 运行测试

```bash
# 运行指定文件中的测试
kaulac test my_tests.kl

# 自动查找当前目录下的测试文件（含 *_test.kl 或包含 #[test] 的 .kl 文件）
kaulac test
```

### 测试输出示例

```
===== Running Tests =====

Running Kaula tests...

test test_addition ... ok
test test_subtraction ... ok
test test_string ... ok
test test_ignored ... IGNORED

===== Test Summary =====
Total:   4
Passed:  3
Failed:  0
Ignored: 1
Time:    0.001s
========================

All tests passed.
```

### `#[ignore]` 忽略测试

使用 `#[ignore]` 属性可以暂时跳过某个测试：

```kaula
#[test]
#[ignore]
fn test_not_ready() {
    // 这个测试不会被执行
    println("this will not run")
}
```

### 注意事项

- 测试函数签名必须是 `fn name()`，无参数无返回值
- 测试函数不需要 `pub` 修饰符，编译器会自动识别
- 测试模式下，原有的 `main` 函数会被测试运行器替代
- 测试函数中的 `println` 输出会直接显示在控制台

---

## `#[derive]` 派生宏

### 概述

`#[derive]` 属性为结构体自动生成常用方法实现，避免重复编写样板代码。目前支持三个 trait：`Debug`、`Clone`、`Eq`。

### 基本用法

```kaula
#[derive(Debug, Clone, Eq)]
struct Point {
    x: i64
    y: i64
}
```

编译器会自动生成以下方法：

| Trait | 生成方法 | 签名 |
|-------|---------|------|
| Debug | `Point_debug` | `fn Point_debug(self: *Point)` |
| Clone | `Point_clone` | `fn Point_clone(self: *Point) -> *Point` |
| Eq | `Point_equals` | `fn Point_equals(a: *Point, b: *Point) -> bool` |

### Debug - 调试打印

自动生成逐字段打印的方法：

```kaula
Point p
p.x = 10
p.y = 20
Point_debug(&p)
// 输出: Point { x: 10, y: 20 }
```

支持的字段类型及打印方式：
- 整数类型（i8/i16/i32/i64/u8/u16/u32/u64）：`%lld`
- 浮点类型（f32/f64）：`%f`
- 布尔类型：`true` / `false`
- 字符串类型：带引号打印
- 其他类型：显示类型名占位

### Clone - 深拷贝

自动生成内存拷贝方法：

```kaula
Point* p2 = Point_clone(&p)
// p2 是 p 的完整副本，修改 p2 不影响 p
```

实现方式：`malloc` 分配新内存 + `memcpy` 拷贝。

### Eq - 相等比较

自动生成逐字段比较方法：

```kaula
Point a
a.x = 5
a.y = 5
Point b
b.x = 5
b.y = 5

if Point_equals(&a, &b) == 1 {
    println("a == b")
}

b.x = 6
if Point_equals(&a, &b) == 0 {
    println("a != b")
}
```

比较规则：
- 整数/布尔：直接 `!=` 比较
- 浮点：直接 `!=` 比较
- 字符串：使用 `strcmp` 比较
- 指针：空指针检查 + 逐字段比较

### 注意事项

- 派生方法名格式为 `{StructName}_{trait}`，不带 `K_` 前缀
- sema 阶段会自动注册派生方法符号，无需手动声明
- 编译器会自动生成前向声明，避免 implicit declaration 警告
- 结构体字段必须有明确的类型声明

---

## `#[command]` 命令行参数解析

### 概述

`#[command]` 属性为结构体自动生成命令行参数解析函数，支持短选项、长选项、默认值和帮助文本。

### 基本用法

```kaula
#[command]
struct Config {
    #[arg(short="v", long="verbose", help="启用详细输出")]
    verbose: bool

    #[arg(short="o", long="output", default="result.txt", help="输出文件路径")]
    output: string

    #[arg(short="n", long="count", default="10", help="处理数量")]
    count: i64
}
```

编译器会自动生成 `Config_parse(argc, argv)` 函数。

### `#[arg]` 字段属性

| 参数 | 说明 | 示例 |
|------|------|------|
| `short` | 短选项名（不带 `-`） | `short="v"` → `-v` |
| `long` | 长选项名（不带 `--`） | `long="verbose"` → `--verbose` |
| `default` | 默认值 | `default="10"` |
| `help` | 帮助文本 | `help="启用详细输出"` |

### 使用示例

```kaula
fn main(i32 argc, char** argv) {
    Config* cfg = Config_parse(argc, argv)
    if cfg == null {
        println("参数解析失败")
        return
    }

    if cfg.verbose == 1 {
        println("详细模式已启用")
    }
    println("输出文件: ")
    println(cfg.output)
}
```

命令行调用：

```bash
./myapp -v -o output.txt -n 42
# 或
./myapp --verbose --output=output.txt --count=42
```

### 支持的字段类型

| 类型 | 解析方式 | 默认值示例 |
|------|---------|-----------|
| `bool` | 出现即设为 1（true） | `default="true"` |
| 整数类型 | `atoll()` 转换 | `default="10"` |
| 浮点类型 | `atof()` 转换 | `default="3.14"` |
| `string` | `string_wrap()` 包装 | `default="hello"` |

### 注意事项

- 布尔选项不需要后跟值，出现即表示 true
- 非布尔选项必须后跟值，否则使用默认值
- `string` 类型使用 `string_wrap()` 零成本包装，不额外分配内存
- 解析函数返回堆分配的结构体指针，使用后需手动释放

---

## 属性组合使用

多个属性可以组合使用，发挥更大威力：

```kaula
#[derive(Debug, Clone, Eq)]
#[command]
struct AppConfig {
    #[arg(short="v", long="verbose", help="详细模式")]
    verbose: bool

    #[arg(short="o", long="output", default="out.txt", help="输出文件")]
    output: string
}

#[test]
fn test_config_defaults() {
    AppConfig* cfg = AppConfig_parse(1, null)
    if cfg.verbose != 0 {
        println("FAIL: verbose should default to false")
    }
    AppConfig_debug(cfg)
}
```

---

## 实现细节

### 编译器集成

高级属性扩展的核心实现在 `compiler/internal/codegen/attribute_features.go`：

- `CollectTestFunctions()` - 收集 `#[test]` 函数
- `GenerateTestRunner()` - 生成测试运行器代码
- `CollectDeriveTypes()` - 收集 `#[derive]` 结构体
- `GenerateDeriveImpls()` - 生成派生方法实现
- `CollectCommands()` - 收集 `#[command]` 结构体
- `GenerateCommandParser()` - 生成命令行解析器

### 编译流程

1. **Parser**：解析 `#[attribute]` 语法，支持连续属性（如 `#[test]#[ignore]`）
2. **Sema**：注册派生方法和命令解析函数符号，跳过测试函数的未使用警告
3. **Codegen**：
   - 收集所有高级属性信息
   - 生成前向声明
   - 在函数代码后插入派生实现和命令解析器
   - 测试模式下用测试运行器替代 main 函数

### 与已有属性的关系

Kaula 已支持的属性（函数/变量/类型级）不受影响：
- 函数：`inline`、`naked`、`section`、`weak`、`deprecated`、`asm`、`no_kmm`、`sor`、`prefix`、`tree`、`root`
- 变量：`volatile`、`aligned`、`section`、`weak`、`deprecated`
- 类型：`packed`、`aligned`、`transparent`

新增属性与已有属性正交，可以自由组合。

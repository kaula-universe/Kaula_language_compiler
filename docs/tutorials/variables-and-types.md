# 变量与类型

Kaula 提供了丰富的内置类型系统和灵活的变量声明方式。

## 基本类型

| Kaula 类型 | C 类型 | 说明 |
|-----------|--------|------|
| `int` | `int64_t` | 默认整数类型 (64位) |
| `i8` / `i16` / `i32` / `i64` | `int8_t` ~ `int64_t` | 定长有符号整数 |
| `u8` / `u16` / `u32` / `u64` | `uint8_t` ~ `uint64_t` | 定长无符号整数 |
| `float` / `f32` | `float` | 单精度浮点 |
| `double` / `f64` | `double` | 双精度浮点 |
| `bool` | `int` | 布尔值 (`true` / `false`) |
| `char` | `char` | 字符 |
| `string` | `char*` | C 风格字符串 |
| `void` | `void` | 空类型 |

## 变量声明

**显式类型声明：**

```kaula
int x = 42
float y = 3.14
string name = "Kaula"
bool flag = true
```

**类型推导使用 `auto`：**

```kaula
auto z = 42        # z 类型推导为 int
auto pi = 3.14     # pi 类型推导为 float
```

> 注意：`auto` 的右侧表达式必须能明确推导类型。

**编译期常量使用 `const`：**

```kaula
const max_retries = 3
const app_name = "Kaula"
```

## 多参数输出

`println` 支持同时输出多个值：

```kaula
println("x = ", x)
println("name = ", name, ", score = ", score)
```

## 完整示例

参见 [examples/variables.kl](examples/variables.kl)。

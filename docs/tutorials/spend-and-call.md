# 强制消费流（spend / call）

`spend` / `call` 是 Kaula 的**强制消费流**语法：把一组元素（数组）或一个枚举值交给 `spend` 块，
要求块内**恰好消费全部元素**。漏消费、重复消费、越界消费都是**编译错误**，
运行时还有一层安全网兜底。

它解决的问题很具体：手写资源释放最大的坑是"漏了一条分支"——`switch` 少一个 `case`、
数组循环少一个元素，编译器一声不吭，资源就泄漏了。`spend` 把"必须全部处理"变成**语言层面的
义务**，由编译器静态证明。

## 基本语法

```kaula
import std.io

fn main() {
    i64 items[3] = [10, 20, 30]
    i64 sum = 0
    spend(items) {
        call(1) { sum = sum + 10 }
        call(2) { sum = sum + 20 }
        call(3) { sum = sum + 30 }
    }
    println("sum = ", sum)   # sum = 60
}
```

| 组成 | 说明 |
|------|------|
| `spend(目标)` | 目标可以是数组变量、数组字面量或枚举值 |
| `call(索引)` | 消费第 N 个元素，索引 **从 1 开始** |
| `call(default)` | 消费所有剩余元素，每个 `spend` 块最多一个 |
| 块内语句 | 与普通块相同，可读写外层变量、`return`、`break` |

## 两种模式

### 数组模式

目标元素个数在编译期确定时进入数组模式。编译期必须能**静态证明全覆盖**：
要么逐个 `call(i)` 覆盖 `1..N`，要么用一个 `call(default)` 兜底。

```kaula
fn main() {
    auto items = [10, 20, 30]
    auto sum = 0
    spend(items) {
        call(1) {
            sum = sum + 10
        }
        call(2) {
            sum = sum + 20
        }
        call(default) {
            sum = sum + 100
        }
    }
    println(sum)   # 130
}
```

编译期无法确定长度的目标（例如函数返回的数组）**必须**提供 `call(default)`，
否则报 `spend_unknown_count`。

### 枚举模式

目标是枚举值时进入枚举模式，语义等同一个**强制穷尽**的 `match`：每个变体都必须有一个
`call(Variant)`，不允许 `call(default)`。

```kaula
enum Color {
    Red
    Green
    Blue
}

fn main() {
    Color c = Color.Green
    auto tag = 0
    spend(c) {
        call(Red)   { tag = 1 }
        call(Green) { tag = 2 }
        call(Blue)  { tag = 3 }
    }
    println(tag)   # 2
}
```

带数据的枚举变体目前不支持（报 `spend_enum_with_data`）。

## 编译期检查

语义分析阶段（`compiler/internal/sema`）完成全集证明，任何一条不满足都直接编译失败：

| 错误码 | 触发条件 | 修复方式 |
|--------|----------|----------|
| `spend_missing_element` | 数组有元素从未被 `call` 消费 | 补 `call(i)`，或加 `call(default)` |
| `spend_index_out_of_range` | `call(i)` 索引超出 `1..N` | 检查索引 |
| `spend_index_less_than_one` | 使用了 `call(0)` 或负索引 | 索引从 1 开始 |
| `spend_duplicate_index` | 同一索引被消费两次 | 删除重复子句 |
| `spend_multiple_default` | 出现多个 `call(default)` | 只保留一个 |
| `spend_default_on_enum` | 枚举模式里写了 `call(default)` | 枚举模式必须显式穷尽 |
| `spend_missing_variant` | 枚举模式漏了某个变体 | 补上对应变体的 `call` |
| `spend_int_on_enum` | 枚举模式里用整数索引 | 改用变体名 |
| `spend_enum_with_data` | 枚举变体带载荷 | 改用 `match` |
| `spend_unknown_count` | 元素个数编译期未知且无 `call(default)` | 加 `call(default)` |
| `spend_early_return` | `return` 出现在非最后一个 `call` 子句中 | 把 `return` 移到最后一个子句 |

示例：

```
bad.kl: Semantic Error: spend_missing_element
spend 未全量消费：元素 2 未被消费（共 3 个元素）
  Suggestion: 添加 call(2) 子句，或使用 call(default) 兜底
```

## 生成的 C 代码

数组模式包装成一个 `Spendable`，走"锁定 → 逐个消费 → 校验 → 释放"的流程：

```c
Spendable* _spd_1 = spendable_create(3);
spendable_add(_spd_1, &(items)[0]);
spendable_add(_spd_1, &(items)[1]);
spendable_add(_spd_1, &(items)[2]);
spend_lock(_spd_1);

spend_call(_spd_1, 1);        /* 标记消费 + 校验 */
{ /* call(1) 的块体 */ }

spend_consume_all(_spd_1);    /* call(default) 的等价物 */
{ /* default 块体 */ }

spend_unlock(_spd_1);         /* 校验 remaining == 0 */
spendable_destroy(_spd_1);
```

枚举模式生成穷尽 `switch`，`default` 分支只是运行时安全网（编译期已证明不可达）。

## 运行时 API

实现在 `src/spend_call.c`，声明在 `src/kaula.h`。这一层是编译器生成代码的内部契约，
**手写 Kaula 代码不需要直接调用**。

| 函数 | 作用 |
|------|------|
| `Spendable* spendable_create(size_t capacity)` | 创建容器，容量至少 1 |
| `void spendable_add(Spendable*, void* component)` | 追加元素，超出容量立即终止 |
| `void spend_lock(void* target)` | 锁定；重复锁定或锁定前已消费 → 终止 |
| `void* spend_call(void* target, int index)` | 消费 1-based 索引元素；未锁定/越界/重复消费 → 终止 |
| `void spend_consume_all(void* target)` | 消费全部剩余元素（`call(default)`） |
| `void spend_unlock(void* target)` | 校验 `remaining == 0`，否则按资源泄漏终止 |
| `void spendable_destroy(Spendable*)` | 释放容器内存 |
| `void* spendable_call(Spendable*)` | 顺序消费下一个未消费元素（legacy 路径，锁定态禁止调用） |

数据结构：

```c
typedef struct Spendable {
    void**   components;  /* 元素指针 */
    size_t   count;       /* 已添加元素数 */
    size_t   capacity;    /* 分配容量 */
    uint8_t* consumed;    /* 消费标记位 */
    size_t   remaining;   /* 剩余未消费数 */
    bool     is_locked;   /* spend_lock 后为 true */
    uint64_t reserved[4]; /* ABI 预留 */
} Spendable;
```

设计取舍：`spend_panic` 直接 `abort()`。理由是这些路径**编译期已证明不可达**，
真的走到说明编译器有缺陷或内存被踩，此时继续执行只会污染更远的状态。
裸机模式下 `spend_call.o` 不参与链接（它依赖 libc）。

## 性能与开销

- 编译期证明是**静态**的，无任何运行时分支判断；
- 运行时只剩三个副作用：容量检查、消标记位写入、`unlock` 时的一次比较；
- `Spendable` 走 `fast_alloc`（bump 分配器），创建/销毁是常数时间。

## 与其他特性的关系

| 特性 | 关系 |
|------|------|
| `match` | `spend` 断言"必须全部处理"，`match` 允许通配；需要强制穷尽时选 `spend` |
| SOR 所有权 | `spend` 与 SOR 正交，可组合使用 |
| KMM 内存管理 | `Spendable` 本身由 `fast_alloc` 分配，与 KMM 作用域无关 |

## 相关文档

- [代码生成器](../code-generation.md#spend-语句-stmtgengo) — 生成细节
- [语义分析](../semantic-analysis.md) — 检查阶段
- [模式匹配](match.md) — `match` 语法
- [配置](../build-config.md) — `spendable_size` 配置项

> 说明：配置文件里的 `spendable_size`（默认 10，命令行 `--spendable`）目前**只被解析和保存，
> 不参与代码生成**——`spendable_create` 的容量始终取编译期确定的元素个数。
> 该选项保留为兼容与后续规划，调整它不会改变 `spend` 的行为。

# KBUG：3 个「静默错误」字面量缺陷 + 1 个迭代缺陷 —— 已修复（附 1 处更正）

> 前 3 个缺陷的共同特征是：**编译通过、零警告，只安静地给出错答案**。
> 比编译失败危险得多——失败会让人立刻知道，静默错数会一路带到基准报告里。
>
> 它们的根因都在 parser / codegen 层：**声明的字面量形状没被识别，值被丢弃**。
> 第 4 个（切片迭代）不属于静默类——它报的是 clang 深处的晦涩类型错误，
> 本轮改成清晰的编译期诊断。

## ⚠️ 一处更正：`#[derive]` 从未损坏

先前本文件把 `#[derive(Debug)]` 列为「缺陷 4」，**那是误判**，特此更正。

实测证据（真基线 = commit `9a334e2`，独立 worktree 编译）：

```kaula
#[derive(Debug, Clone, Eq)]
struct Point { x: i64, y: i64 }
Point p
p.x = 10
p.y = 20
Point_debug(&p)     // 基线就输出 Point { x: 10, y: 20 } ✅
```

这是 `docs/tutorials/examples/advanced-attributes.kl` 里的官方写法——**逐字段赋值**，
它在修复前、修复后都完全正常。

先前之所以观察到 `Point { x: 0, y: 0 }`，是因为用了 `Point { x = 10, y = 20 }`
这种 **struct 字面量**写法，而那个写法本身有缺陷（见下方缺陷 3）。
derive 只是第一个把下游坏值暴露出来的消费者，不是缺陷源头。

结论：**derive 无需任何修复**。本轮改动对 derive 生成的代码零改动。

---

## 缺陷 1：`[]i32` 数组字面量元素类型错误

### 症状

```kaula
[]i32 arr = [1, 3, 1990]
arr[0] + arr[1] + arr[2]   // 期望 1994，实际 4 ❌
```

### 根因

`generateArrayLiteral`（`exprgen.go`）硬编码复合字面量元素类型：

```c
int32_t* arr = ((int64_t[]){ 1, 3, 1990 });   // 元素 8 字节，指针步长 4 字节
```

指针按 8 字节步长走、元素只占 4 字节 → `arr[1]` 读到的是第 0 槽的高 4 字节。
`[]i64` 恰好两者一致，所以只有窄类型触发。

### 修复

新增 `pendingArrayElemType` 上下文（`codegen.go`），在数组声明的各条路径
（指针分支 / 运行期长度分支）按元素类型设置，`generateArrayLiteral` 据此
选择复合字面量的元素 C 类型；无上下文时回退 `int64_t`（保持旧行为）。

### 验证

| 声明 | 修复前 | 修复后 |
|---|---|---|
| `[]i32 arr = [1,3,1990]` | 4 | **1994** ✅ |
| `[]i64 arr = [1,3,1990]` | 1994 | 1994 ✅ |
| `[]u8 b = [65,66,67]` | 错 | **198** ✅ |
| `[]f64 v = [1.5,2.5,3.0]` | 错 | **7.0** ✅ |

---

## 缺陷 2：定长数组 `{...}` 初始化器被丢弃

> **这条有官方文档背书**，不是我的臆测写法。
> `docs/tutorials/control-flow.md:76`「迭代数组/切片」一节原样写着：
> ```kaula
> int[5] arr = {10, 20, 30, 40, 50}
> for x in arr { println("x = ", x) }
> ```
> （顺带记一笔：文档里的类型名 `int` 是笔误，语言里没有 `int`，应为 `i32`。）

### 症状

```kaula
i32 arr[5] = {10, 20, 30, 40, 50}
i32 s = 0
for x in arr { s = s + x }   // 期望 150，实际 0 ❌
```

生成的 C：

```c
int32_t arr[5] = {0};        // 初始化值没了
```

### 根因

parser 的 `case lexer.TOKEN_LBRACE`（表达式入口）**无条件**把 `{...}` 当结构体
字面量。而 `parseStructLiteralExpression` 只认 `.field = value`，遇到裸值
`10, 20, ...` 只是 `p.nextToken()` 跳过 → `Fields` 为空 → codegen 回落 `{0}`。

### 修复

按 `{` 后的首个 token 分派：

- `.` → `parseStructLiteralExpression`（结构体字面量）
- 否则 → 新增 `parseBraceArrayLiteral`（花括号数组初始化器，产出 `*ast.ArrayLiteral`）

### 验证

`i32 arr[5] = {10,20,30,40,50}` + `for x in arr` 求和：**0 → 150** ✅

---

## 缺陷 3：struct 字面量无点赋值被**静默丢弃**

> **诚实标注**：`TypeName { field = value }` 这种「无点」写法**没有**出现在任何官方文档或
> 语料库样例里——官方只有 `{ .field = value }` 带点形式（`docs/tutorials/structs-and-enums.md:38`）
> 和逐字段赋值。所以严格说，这不是「文档承诺的功能坏了」。
>
> 但问题在于 parser 遇到它时的行为：**不报错、不警告，直接把整个初始化列表吞掉**，
> 产出一个全零对象。用户手写漏一个点，损失全部字段值且毫无提示——这才是真缺陷。
> 修完之后两种写法都合法，不存在「静默丢弃」这条路径。

### 症状

```kaula
struct Point { x: i64, y: i64 }
Point p = Point { x = 10, y = 20 }
p.x   // 期望 10，实际 0 ❌
```

生成的 C：

```c
K_Point p = (K_Point){0};
```

### 根因

`parseStructLiteralWithType`（`TypeName{...}`）只识别两种字段写法：

- `.field = value`（带点）
- `field: value`（冒号）

文档与用户实际都在写的 `field = value`（**无点、等号**）落到 `else` 分支被静默丢弃。

### 修复

在 `parseStructLiteralWithType` 与 `parseStructLiteralExpression` 两处都用
补一个分支：`IDENT` 后跟 `ASSIGN` → 正常收集为字段。两种写法现在都支持。

### 验证

`Point p = Point { x = 10, y = 20 }` → `p.x == 10 && p.y == 20` ✅
带点写法 `Point { .x = 7, .y = 8 }` 同样正确 ✅

---

## 缺陷 4：`[]T` 切片迭代生成坏 C

### 症状

```kaula
[]i64 arr = [1, 3, 1990]
for v in arr { s = s + v }
```

```
<stdin>:36:37: error: member reference base type 'int64_t *'
                       is not a structure or union
```

### 根因

`[]T` 的 C 类型是**裸指针**（`int64_t*`），但 `generateForInStatement` 把
`isFixedArray` 对 `[]T` 也置为 true，长度表达式回落到 `(arr).len` ——
对裸指针取成员。切片的长度在运行期本来就不可得。

### 修复

区分 `[N]T`（定长，长度编译期已知）与 `[]T`（切片，无长度）。
对后者给出**清晰的编译期错误**，而不是让 clang 在生成的 C 里报栈底信息：

```
无法迭代切片 `[]T`（行 5）：其 C 侧是裸指针，运行期不带长度。
请改用定长数组 `[N]T`，或用下标循环 `for i in range(n) { arr[i] }`。
```

### 验证

`for v in arr`（`[]i64`）现在得到上述明确错误，不再是 clang 的类型报错 ✅
定长数组 `for x in arr` 正常工作 ✅

---

## 回归测试

### 单元测试（`compiler/internal/codegen/literal_codegen_regression_test.go`）

| 测试 | 守护 |
|---|---|
| `TestArrayLiteralElementTypeMatchesDeclaration` | 缺陷 1：`[]i32`/`[]i64`/`[]u8` 复合字面量元素类型 |
| `TestBraceArrayLiteralParsedAsArray` | 缺陷 2：`{...}` 解析为 ArrayLiteral + 初始化值进 C |
| `TestStructLiteralDotlessFieldAssign` | 缺陷 3：无点字段赋值 |
| `TestSliceIterationRejectedClearly` | 缺陷 4：切片迭代给出清晰错误 |

**已验证守护力（真基线 = commit `9a334e2` 的独立 worktree）**：把测试文件拷进基线
worktree 跑，4 个测试**全部 FAIL**；在修复后的代码上**全部 PASS**。

> 早先一次声称的「修复前全 FAIL」是用错误基线（`git stash` 时改动已入库）得出的，
> 结论无效；上面的数据已用真基线重做。

### 端到端语义测试（`test/literal_semantics.kl`）

覆盖上述 4 个缺陷，退出码 0 即通过。由 `scripts/run_tests.py` 自动发现，
已纳入测试矩阵。该文件头部注释明确记录了「官方定长数组写法用方括号」以及
「`#[derive]` 从未损坏」两条更正，避免后来者重踩。

### 全量结果

| 项 | 结果 |
|---|---|
| `go test ./...` | ✅ 全绿（14 个含测试包） |
| `scripts/run_tests.py` | **104/106**（余 2 个 `stdlib_async_safety` 为**预存失败**，真基线同样 -11） |
| 官方 `docs/tutorials/examples/*.kl` | ✅ 10/10 编译通过 |
| 回归单测守护力 | 8 个测试在真基线（`9a334e2`）上**全部 FAIL**，修复后全部 PASS |
| Kaula 语言层服务端到端 | ✅ 4 条请求与基线响应**逐字节一致**（如 `O,7,SKU-0001,105,57` → `R,7,e9da68a0,5985`） |

> 注：早先记录的 `R,7,65055505,5970` 是另一条请求串的结果，本轮改用可复现的对照方式重测。

---

## 改动文件

| 文件 | 改动 |
|---|---|
| `compiler/internal/codegen/codegen.go` | 新增 `pendingArrayElemType` 字段 |
| `compiler/internal/codegen/exprgen.go` | `generateArrayLiteral` 按上下文选元素类型 |
| `compiler/internal/codegen/stmtgen.go` | 数组声明路径设置元素类型上下文；切片迭代给出清晰错误 |
| `compiler/internal/parser/parser.go` | `{` 分派 + `parseBraceArrayLiteral`；两处无点字段赋值 |
| `compiler/internal/codegen/literal_codegen_regression_test.go` | **新增** 4 个单元测试 |
| `test/literal_semantics.kl` | **新增** 端到端语义测试 |

> **同批还修了一组泛型 / ADT 缺陷**（`docs/tutorials/generics.md` 里 `Option<T>`、
> `Result<T,E>`、`class Box<T>` 三个例子原先都编不过），
> 与本文的字面量缺陷无重叠，单独记录在
> [`KAULA_FEATURES.md` §3.2](./KAULA_FEATURES.md#32-五个泛型--adt-缺陷)，
> 回归测试见 `compiler/internal/codegen/generic_codegen_regression_test.go`。

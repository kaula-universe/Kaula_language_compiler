# P2 — 编译器长加法链的指数爆炸（O(2ⁿ) → O(n)）

> 状态：**已修复并合入 master**
> 影响面：`compiler/internal/codegen/exprgen.go`（前端代码生成）
> 附带修复：release 模式下 `+` 操作数丢失括号导致的**静默语义错误**
> 护栏：`compiler/internal/codegen/plus_codegen_regression_test.go`、`test/codegen_plus_chain_scale.kl`

---

## 1. 现象

编译一个含长左结合 `+` 链的 Kaula 源文件时，卡住不返回：

| 用例 | 变量/项数 | 修复前耗时 |
|------|-----------|------------|
| `a28.kl` | 28 个 `+` | **> 12 分钟**（120s 超时未出结果） |
| `n48 / n96` | 48 / 96 个 `+` | 同上，超时 |
| `b48.kl` | 48 变量、96 条**独立**语句 | **0s** |

关键对照：`b48` 有 48 个变量却瞬间编完，`a28` 只有 28 个变量却卡死 ——
**与变量数无关，与表达式链长度有关**。

gdb 附着卡住的进程，`thread apply all bt` 显示调用栈深度 99，且
`generateBinaryExpression` ↔ `GenerateExpression` 交替重复出现。

---

## 2. 定位

临时插桩（`generateBinaryExpression` 入口计数 + 同节点二次生成时打印调用栈），
得到决定性证据：

```
28 个节点的表达式 → generateBinaryExpression 被调用 24,000,000+ 次
（正确值应为 28 次）
```

重复生成的调用栈：

```
generateBinaryExpression(exprgen.go:677)      → generatePlusOperation(...)
  generatePlusOperation(exprgen.go:795-796)   → GenerateExpression(子树)   ← 再生成一遍
```

---

## 3. 根因

`generateBinaryExpression` 在函数前段已经把左右子树生成好了：

```go
// 行 ~534
left  := eg.GenerateExpression(e.Left)
right := eg.GenerateExpression(e.Right)
```

但 `PLUS` 分支把这两个结果**丢掉**，转而去调 `generatePlusOperation(e.Left, e.Right)`，
而后者内部又对两棵子树各生成了一次：

```go
// 行 ~677（修复前）
case "PLUS", "+":
    return eg.generatePlusOperation(e.Left, e.Right)   // ← 丢弃 left/right

// 行 ~792（修复前）
func (eg *ExpressionGenerator) generatePlusOperation(left, right ast.Expression) string {
    leftType  := eg.inferType(left)
    rightType := eg.inferType(right)
    leftStr  := eg.GenerateExpression(left)    // ← 重复生成
    rightStr := eg.GenerateExpression(right)   // ← 重复生成
    ...
}
```

每棵子树被生成 **2 次**，递归下去即：

```
T(n) = 2 · T(n-1)  ⇒  T(n) = O(2ⁿ)
```

`+` 是左结合的，n 个 `+` 就是 n 层嵌套，全部命中这条路径。
**其它运算符（MINUS / MULTIPLY / DIVIDE / 位运算…）都直接用现成的
`left` / `right`，只有 `PLUS` 有这个毛病。**

---

## 4. 修复

让 `generatePlusOperation` 接收调用方已生成好的字符串，不再自己生成：

```go
// 行 ~677（修复后）
case "PLUS", "+":
    return eg.generatePlusOperation(e.Left, e.Right, left, right)

// 行 ~792（修复后）
func (eg *ExpressionGenerator) generatePlusOperation(
    left, right ast.Expression, leftStr, rightStr string) string {
    leftType  := eg.inferType(left)
    rightType := eg.inferType(right)
    // 不再调用 eg.GenerateExpression(...)
    ...
}
```

同时在两处留下不变量注释，明确"**任何分支都不得再对子树调 GenerateExpression**"。

---

## 5. 附带修复：release 模式下的静默语义错误

复用调用方的 `left` / `right` 还顺带拿到了调用方的
`astBinaryNeedsParens` 括号修正 —— **此前 `PLUS` 是唯一绕过该修正的分支**。

C 里 `|` 的优先级低于 `+`，于是 release 模式（关闭整数溢出检查、走裸
`" + "` 拼接）下：

```c
/* 修复前：(a | b) + c 被生成成 */
int64_t r = a | b + c;      /* C 解析为 a | (b + c)  ← 语义错了 */

/* 修复后 */
int64_t r = (a | b) + c;
```

实测（a=3, b=5, c=1）：

| 版本 | 生成代码 | 运行结果 | 正确值 |
|------|----------|----------|--------|
| 修复前（release） | `a \| b + c` | **r=7** ❌ | 8 |
| 修复后（release） | `(a \| b) + c` | r=8 ✅ | 8 |

**为什么之前没人发现**：debug 模式默认开启整数溢出检查，会把表达式包成
`kaula_add_i64((int64_t)(a | b), ...)`，cast 自带的那对括号恰好掩盖了
缺失的括号。所以这个错误**只在生产构建（`-release` / `-O3`）出现**，
是典型的"测试环境全绿、线上静默算错"。

右侧同理：`c + (a | b)` 修复前生成 `c + a | b`（C 按 `(c + a) | b` 解析）。

---

## 6. 实测：修复前后耗时对比

同一组左结合 `+` 链（`as<i64>(0) + as<i64>(1) + ...`），`-release`，
端到端（含 clang 编译 + 链接）：

| 链长 | 修复前 | 修复后 | 加速比 |
|------|--------|--------|--------|
| 16 | 0.20 s | 0.18 s | 1.1× |
| 20 | 0.52 s | 0.19 s | 2.7× |
| 22 | 1.55 s | 0.18 s | 8.6× |
| 24 | 5.59 s | 0.19 s | 29× |
| 26 | 21.77 s | 0.18 s | 121× |
| 28 | **86.94 s** | 0.19 s | **458×** |
| 50 | 不可完成 | 0.19 s | — |
| 100 | 不可完成 | 0.19 s | — |
| 200 | 不可完成 | 0.21 s | — |

每增加 2 个节点耗时约 ×3.5（≈ 2²），是教科书式的 O(2ⁿ) 曲线；
修复后固定在 ~0.19s（剩余全是 clang 的固定开销），与链长无关。

按该曲线外推，修复前 40 项约需 **44 小时**。

---

## 7. 生成代码的等价性验证

修复必须保证**不改语义**。对 `test/` 下全部 114 个 `.kl` 用修复前/后的
编译器各生成一次 C，逐行比对。

难点：生成结果本身带非确定性（`#include` 顺序、warning 输出顺序都受
map 迭代顺序影响）。先量化噪声基线：

| 对比 | 原始 diff | 行多重集 diff |
|------|-----------|----------------|
| new vs new2（同一二进制跑两次） | 28 个文件有差异 | **0** |
| old vs new（修复前后） | 38 个文件有差异 | **5** |

行多重集比较完全消除顺序噪声（`new vs new2 = 0` 证明该指标稳定），
因此 old vs new 的 5 个差异是**真实内容差异**。逐个审查，全部是同一类：

```diff
- (int64_t)(line[...] - 48)
+ (int64_t)((line[...] - 48))
```

即第 5 节所述的括号补全 —— 保守加括号不改变语义，且与
MINUS / MULTIPLY 等其它分支的行为对齐。**无其它差异。**

---

## 8. 护栏

回归防护分两层，都已验证"修复前必挂、修复后必过"。

### 8.1 白盒单测 `plus_codegen_regression_test.go`

| 测试 | 内容 |
|------|------|
| `TestPlusChainIsNotExponential` | 30 层 `+` 链必须在 **2s** 内生成完（回归则立刻超时，不挂住 CI）；并断言每个操作数只出现 1 次 |
| `TestPlusOperandKeepsParentheses` | `(a \| b) + c` 在 release 模式必须保留括号 |
| `TestPlusRightOperandKeepsParentheses` | `c + (a \| b)` 同上 |

修复前的实测输出（三条全 FAIL）：

```
--- FAIL: TestPlusChainIsNotExponential (2.02s)
    release=false: 30 层 + 链生成超过 2s —— 指数爆炸回归
--- FAIL: TestPlusOperandKeepsParentheses
      得到: "a | b + c"      期望: "(a | b) + c"
--- FAIL: TestPlusRightOperandKeepsParentheses
      得到: "c + a | b"      期望: "c + (a | b)"
```

修复后：30 层链 debug 749µs / release 101µs，三条全 PASS。

### 8.2 端到端 `test/codegen_plus_chain_scale.kl`

40 项左结合 `+` 链，断言 `sum == 780`。重复生成一旦回归，编译必然撞上
`run_tests.py` 的 180s 超时（修复前 40 项约需 44 小时），护栏立刻亮红灯。

---

## 9. 验收

| 项目 | 结果 |
|------|------|
| `go test ./...`（compiler） | 全绿（含 3 条新增） |
| `scripts/run_tests.py` | **114/114**（原 112 + 新增 1 程序 × 2 模式） |
| `scripts/run_tests.py --quick` | **98/98** |
| `test/perf/run_perf.py` | **6/6** |
| `alloc_bench -sor` 峰值 RSS | 10088–10112 KB（修复前基线 10124 KB，**无回归**） |
| 运行时结果 | chain50=1225 / chain100=4950 / chain200=19900，全部正确 |

---

## 10. 遗留观察（未处理，无风险）

1. **clang 括号嵌套上限**：200 项以上的 `+` 链在 **debug** 模式下会撞上
   clang 的 `bracket nesting level exceeded maximum of 256` —— 因为溢出检查
   每层套一个 `kaula_add_i64((int64_t)(...))`。release 模式无此问题
   （200 项实测 0.21s）。属既有行为、极端场景，未改动。
2. **复合赋值的 2× 重复生成**：`a += b` 会展开成 `a = a + b` 的 AST，
   其中 `e.Left` 出现两次，故左值子树生成 2 次。这是**常数**因子（不是指数），
   且左值只能是标识符/下标等无副作用形式，暂不改动以规避回归风险。
3. **生成器输出非确定性**：`#include` 顺序与 warning 顺序受 map 迭代顺序
   影响，同一输入两次运行产物不同行序。这使"golden C 比对"类护栏不可行，
   需改用行多重集或语义归一化比较。

# Kaula 前端严格化方案（Parser + Sema 前置拦截）

> 状态：Draft · 2026-10-25
> 作者：壹米佰叁（zhe116）
> 目标：把"错误漏到 codegen / clang 才炸"的路径，收敛为"前端第一时间报错并 fail-fast"。

---

## 1. 背景与问题诊断

### 1.1 现象

用户在写 Kaula 时遇到的典型痛点：

- 写错一个参数数量 / 参数类型，前端不报错，kaulac 安静生成一段 C，最后由 **clang** 抛出
  `too few arguments to function 'add'` / `incompatible type for argument 1`。
- 访问 struct 不存在的字段 `p.zzz`，前端不报错，clang 报
  `'Point' has no member named 'zzz'`。
- 函数声明 `fn f() i32`，函数体 `return "not an int"`，前端不报错，clang 报
  `incompatible types when returning type 'String' but 'int32_t' was expected`。
- 同作用域重复声明 `i32 x = 1; i32 x = 2;`，前端只给一个 `unused_but_set` 警告，
  clang 报 `redefinition of 'x'`。

**结果**：用户看到的是 C 编译器的英文报错，定位距离根因隔着一层，且常常不知道是
Kaula 哪一行、哪个符号出了问题。

### 1.2 数据证据（2026-10-25 在 master @ `18b125a` 实测）

| 用例 | 前端（lex/parse/sema）错误 | 实际报错位置 | 漏报？ |
|---|---|---|---|
| 本地函数调用参数数量不匹配 | 0 | clang: `too few/many arguments to function 'add'` | **是** |
| 本地函数调用参数类型不匹配 | 0 | clang: `incompatible type for argument 1 of 'takes_int'` | **是** |
| 访问 struct 不存在字段 | 0 | clang: `'Point' has no member named 'zzz'` | **是** |
| 变量赋值类型不兼容（`i32 n; n = "hi"`） | 0（仅 unused 警告） | clang: `incompatible types when assigning ... from type 'String'` | **是** |
| 函数返回类型不匹配（声明 `i32` 却 `return "str"`） | 0 | clang: `incompatible types when returning type 'String' but 'int32_t' was expected` | **是** |
| 同作用域重复声明同名变量 | 0（仅 unused 警告） | clang: `redefinition of 'x'` | **是** |
| return 后写不可达语句 | 0 | clang: `void value not ignored as it ought to be` | **是** |
| 未定义变量 `println(undefined_var)` | 1 ✓ | — | 否（已正确拦截） |

### 1.3 结构性根因

#### 根因 A：codegen 体量与 sema 体量严重失衡

```
codegen/   39,678 行
sor/        8,141 行
parser/     8,010 行
sema/       6,118 行
lexer/      1,482 行
symbol/       216 行   ← 符号表极小
semantic/     410 行
```

**codegen 是 sema 的 6.5 倍**。这意味着大量"本该在前端做的检查"被推到了 codegen
里以"猜测+兜底"的方式处理，而 codegen 一旦猜错就直接产出错误 C。

#### 根因 B：sema 对 stdlib 调用严、对本地函数调用松

`sema.analyzeCallExpression` 中：

- 走 stdlib 分支（`std.<mod>.<fn>`）时，**已经**做了：
  - 模块导入检查
  - 参数数量检查（`wrong_arg_count`）
  - 未解析函数名报错（`reportUnresolvedStdlibCall`）
- 走本地用户函数分支时，**只**做了：
  - 未定义函数名报错（`undefined_function`）
  - 可空性检查（`nullable_passed`）
  - adopt/unwrap 套用类型边界检查
- **没有做**：
  - 本地函数实参数量 vs 形参数量校验
  - 本地函数实参类型 vs 形参类型校验
  - 返回值类型与声明返回类型校验
  - 把 void 函数返回值赋给变量
  - 把非函数标识符当函数调用

#### 根因 C：pipeline 没有 fail-fast gate

`cmd/kaulac/main.go` 的主流程：

```
Stage 1: lex + parse       → errorCollector
Stage 2: sema.Analyze      → errorCollector
Stage 2.5: SOR / 逃逸分析
Stage 3: cg.Generate(program)   ← 无条件执行，不看 Stage1/2 是否有错
        ↓
        再统计 errorCount
        ↓
        if errorCount > 0 { 跳过 clang 编译 }
```

也就是说：**即使 sema 已经报错，codegen 还是先跑完了**。这虽然在最终 exit code
上是对的（错误会阻止 clang 跑），但：

1. codegen 在错误 AST 上跑一遍是浪费；
2. codegen 自己内部也会加诊断（`cg.Errors()`），把"前端错"和"codegen 兜底错"
   混在一起输出，用户分不清；
3. 真正危险的是 **sema 漏报** 的那部分——sema 没报错，codegen 就以为 AST 是
   干净的，放心生成 C，于是错误一路漏到 clang。

#### 根因 D：符号表（symbol/）只有 216 行

类型信息、函数签名、struct 字段表这些"前端严格检查"赖以生存的数据结构，当前
大量散落在 sema.go 的局部 map 和 `inferExpressionType` 的临时推断里，没有一个
集中的、可查询的"程序类型环境（type environment）"。这导致：

- 同一个"查询 Point 的字段"逻辑，sema 写一份、codegen 写一份、sor 再写一份；
- 任何新增的严格检查都要重新推断类型，重复劳动且容易不一致。

---

## 2. 设计目标

1. **P0 硬目标**：本文 §1.2 表格里所有"漏报"用例，必须在**前端**报错，错误码
   必须是 Kaula 自己的（`wrong_arg_count_local` / `arg_type_mismatch` /
   `no_such_field` / `return_type_mismatch` / `redeclaration` 等），且
   **exit code != 0，clang 不跑**。
2. **P0 硬目标**：sema 报错后，codegen **不得**再跑（fail-fast gate）。
3. **P1 目标**：建立集中的"类型环境"（`types.Env`），把函数签名、struct 字段、
   类型别名的查询收口；sema / codegen / sor 共用。
4. **P1 目标**：stdlib 自身（60 目录 / 67 模块 / 1547 函数）与 test/ 语料库
   （~150 个 .kl）在新检查下 **零新增错误**——要么检查本身对 stdlib 豁免，
   要么修 stdlib 源码。
5. **P2 目标**：新增 `--strict` 开关（默认开）；旧代码可以临时 `--no-strict`
   回退到"只警告不报错"，作为迁移缓冲。

---

## 3. 分阶段方案

### 3.1 Phase 0：Fail-fast gate（半天，先把骨架立住）

**改动点**：`cmd/kaulac/main.go`

在 Stage 2 结束后、Stage 3 之前插入：

```go
// 新增：前端错误 fail-fast gate
if n := len(stage1Errs) + len(stage2Errs); n > 0 {
    // 打印已收集的诊断
    printFrontendDiagnostics(stage1Errs, stage2Errs, nil)
    fmt.Fprintf(os.Stderr, "\n错误: 前端 %d 个错误，已中止代码生成\n", n)
    os.Exit(1)
}
```

**收益**：
- 立即消除"codegen 在错误 AST 上白跑一遍"的浪费；
- 让后续新增的 sema 检查天然就是"硬错误"，不会再被 codegen 兜底掩盖。

**风险**：极低。当前 pipeline 本来就在 `errorCount > 0` 时跳过 clang，只是把
"提前到 codegen 之前"。

**验收**：
- 故意写一个 `undefined_var`，确认 kaulac 输出前端错误后立即退出，**不再**
  出现 `[Cache] 编译中` / `Generated Code` 字样。

---

### 3.2 Phase 1：补 P0 漏报检查（核心，1–2 周）

按"用户痛点频率 × 修复成本"排序，逐个加 sema 检查。

#### 3.2.1 本地函数调用：参数数量校验

**位置**：`sema.analyzeCallExpression`，本地函数分支末尾。

**规则**：
- 拿到被调用函数的 AST（`findFunctionDeclaration(name)` 已有）；
- 比对 `len(expr.Args)` vs `len(fn.Params)`；
- 不匹配 → `AddSemanticError`，错误码 `wrong_arg_count_local`；
- 提示里要打印形参签名，例如：
  ```
  函数 'add' 接受 2 个参数 (i32 a, i32 b)，但传了 1 个
  ```

**豁免**：
- `range(...)` / `adopt(...)` / `unwrap(...)` 内建；
- 变参函数（当前语法若有 `...`）；
- stdlib 函数已有分支，不重复检查；
- 跨文件 import 的函数（`localImportFuncs` / `localModuleFuncs`），暂时放行
  （跨文件签名在 P1 做）。

#### 3.2.2 本地函数调用：参数类型校验

**位置**：同上。

**规则**：
- 对每个实参，用 `inferExpressionType(arg)` 推出 Kaula 类型字符串；
- 与形参类型比对；
- 不兼容 → `arg_type_mismatch`，提示：
  ```
  函数 'takes_int' 的第 1 个参数期望 i32，实际传入 string
  ```

**类型兼容规则**（先做最小集合）：
- 同类型 ✓；
- 整数家族（i8/i16/i32/i64/u8/...）互相隐式允许（与 C 一致）；
- 浮点家族（f32/f64）互相允许；
- i32 → f64 允许（宽化）；
- 其他一律报错；
- `nullable T` 传给非可空形参 → 已有 `nullable_passed`，不重复。

**豁免**：
- 实参已经是 `adopt(x)` / `unwrap(x)` 形式 → 已有 adopt 检查接管；
- stdlib 调用走原分支。

#### 3.2.3 返回类型校验

**位置**：`sema.analyzeReturnStatement` + 函数级收尾。

**规则**：
- 函数声明返回类型 `T`；
- 函数体里每个 `return <expr>`，检查 `inferExpressionType(expr)` 与 `T` 兼容；
- 不兼容 → `return_type_mismatch`；
- `fn f() void` 函数里出现 `return <expr>` → `return_value_in_void_fn`；
- 非 void 函数末尾没有 return（且没有 if/else 全覆盖 return）→
  `missing_return`（先做警告，P1 末段升错误）。

**豁免**：
- `#[noreturn]` 标注的函数；
- 测试函数 `#[test]`。

#### 3.2.4 结构体字段访问校验

**位置**：`sema.analyzeMemberExpression` / `analyzeMemberAccess`。

**规则**：
- `obj.field` 中，先推 `obj` 的类型 `T`；
- 若 `T` 是 struct / class，查表 `structFields[T]`；
- `field` 不在字段表里 → `no_such_field`，并列出相似字段名（levenshtein ≤ 2）；
- 提示：
  ```
  类型 'Point' 上没有字段 'zzz'；是否想写 'x' 或 'y'？
  ```

**豁免**：
- `obj` 是 dynamic/object/any 类型；
- 跨文件 struct（P1 再做）；
- `std.<mod>.<fn>` 链上的段（已有独立分支）。

#### 3.2.5 赋值类型兼容校验

**位置**：`sema.analyzeVariableDeclaration`（`n = s` 形式）+ 未来的
assign 语句分析。

**规则**：
- 左值类型 `TL`，右值类型 `TR`；
- 不兼容 → `assign_type_mismatch`；
- 同 3.2.2 的兼容规则。

#### 3.2.6 同作用域重复声明

**位置**：`sema.analyzeVariableDeclaration`。

**规则**：
- 当前作用域的符号表已存在同名 → `redeclaration`，**错误**（不是警告）；
- 提示里指向上一次声明的行号。

**注意**：当前 `checkVariableShadow` 是做"内层 shadow 外层"的警告，要和"同层
重复声明"区分开：
- 同层重复声明 → 错误；
- 内层 shadow 外层 → 警告（保留现状）。

#### 3.2.7 调用非函数标识符

**位置**：`sema.analyzeCallExpression`。

**规则**：
- 被调用方解析后是一个变量符号，且符号类型不是函数/函数指针 →
  `calling_non_callable`；
- 例如 `i32 n = 42; n(1,2)` → 报：
  ```
  'n' 的类型是 i32，不是函数，不能调用
  ```

---

### 3.3 Phase 2：集中类型环境（P1，2–3 周）

新建 `internal/types/env.go`（约 800–1200 行），收口：

```go
package types

type Env struct {
    Funcs      map[string]*FuncSig       // 顶层函数
    Structs    map[string]*StructDecl    // struct 字段表
    Classes    map[string]*ClassDecl
    Interfaces map[string]*InterfaceDecl
    TypeAliases map[string]*TypeAlias
    Vars       map[string]*VarInfo       // 全局变量
}

type FuncSig struct {
    Params []Param
    Ret    string
    VarArgs bool
    Pos    ast.Position
}

type StructDecl struct {
    Fields []Field
    Pos    ast.Position
}
```

**改动**：
- `sema.Analyze` 第一遍收集时把符号写进 `*types.Env`；
- codegen 通过 `program.TypesEnv` 读，不再自己重复扫 AST；
- sor 通过同一份 env 读，去掉自己的"再扫一遍"逻辑。

**收益**：
- 3.2 里所有检查都从"临时推断"变成"查 env"，准确率提升；
- codegen 与 sema 不再各写一份"找 Point 的字段"逻辑，消除不一致；
- 为跨文件（多 TU）签名检查打基础。

---

### 3.4 Phase 3：跨文件 + 多 TU 严格化（P1 收尾，1 周）

- 本地 import 的函数，在 TU 边界上做参数数量/类型校验；
- 跨文件 struct 字段校验；
- 跨文件 enum variant 完备性校验。

---

### 3.5 Phase 4：CI gate + `--strict` 开关（P2，2 天）

- `scripts/run_tests.py` 加一个 `--expect-frontend-errors` 模式：
  对 `test/errors/*.kl` 这类"故意写错"的用例，断言 kaulac **以指定错误码退出**；
- 新增 `test/errors/` 目录，把本文 §1.2 的 7 个漏报用例收编为回归测试；
- `--strict` 默认 on；`--no-strict` 把新错误降级为警告，作为旧代码迁移缓冲；
- CI（`.github/workflows/`）加一步：`kaulac --strict` 对 stdlib 全部 .kl
  跑一遍，必须 0 错误。

---

## 4. 影响面评估与迁移策略

### 4.1 stdlib 自身（67 模块 / 1547 函数）

**风险**：新检查可能让 stdlib 自己编不过。

**策略**：
1. Phase 1 每加一个检查，立刻 `python3 toolkit_build.py` 重编 stdlib；
2. stdlib 源码放在 `std/` 下，发现问题**直接修 stdlib**（不是放宽检查）；
3. 对确实合理的"历史写法"（例如老代码用 `i32` 接 `i64` 返回值），在检查里
   加窄豁免，豁免必须写注释说明原因。

### 4.2 test/ 语料库（~150 个 .kl）

**风险**：stress_* / mem* 等测试可能本来就靠"宽松检查"过。

**策略**：
- `python3 scripts/run_tests.py --quick` 作为每轮 gate；
- 新增检查导致旧测试失败时，**优先修测试**，不要回退检查；
- 测试失败的修复必须单独 commit，commit message 写
  `test: align <case> with new <check_name>`。

### 4.3 第三方 pkglib 包

**策略**：Phase 1 期间对 pkglib 函数调用**暂不做参数类型校验**（只做数量），
类型校验推到 Phase 3。避免生态爆炸。

---

## 5. 验收标准（Definition of Done）

Phase 1 完成时，必须满足：

1. §1.2 表格里 7 个漏报用例，全部在前端报错，错误码为 Kaula 自有码；
2. 前端有错时，codegen 不再跑（grep 输出无 `Generated Code` / `[Cache] 编译中`）；
3. `python3 scripts/run_tests.py --quick` 全绿；
4. `python3 toolkit_build.py` 全绿（stdlib / freestanding / runtime / kaulac）；
5. `test/errors/` 目录下有对应回归测试，每个用例断言精确错误码；
6. `CHANGELOG.md` 增补一节"前端严格化"，列出新增错误码与迁移指南；
7. 文档：
   - 本文档（`docs/frontend-strictness-plan.md`）标记为 Accepted；
   - `docs/semantic-analysis.md` 增补"本地函数调用校验"小节；
   - `docs/error-handling.md` 增补新错误码表。

---

## 6. 新增错误码清单（Phase 1）

| 错误码 | 级别 | 触发场景 |
|---|---|---|
| `wrong_arg_count_local` | error | 本地函数实参数量 ≠ 形参数量 |
| `arg_type_mismatch` | error | 本地函数实参类型不兼容形参 |
| `return_type_mismatch` | error | return 表达式类型 ≠ 函数声明返回类型 |
| `return_value_in_void_fn` | error | void 函数返回了值 |
| `missing_return` | warning → error（P1 末段） | 非 void 函数无 return 路径 |
| `no_such_field` | error | struct/class 字段不存在 |
| `assign_type_mismatch` | error | 赋值左右类型不兼容 |
| `redeclaration` | error | 同作用域重复声明同名变量 |
| `calling_non_callable` | error | 调用非函数类型的标识符 |

---

## 7. 不做什么（Out of Scope）

为了避免范围爆炸，本轮**不做**：

- 自动类型推导（`auto x = ...` 的语义级重写，当前已基本可用）；
- 泛型实例化的类型参数严格校验（现有 `validateGenericInstantiation` 已在）；
- SOR 所有权规则的新增（sor/ 已经 8k 行，本轮不动）；
- 跨语言 FFI 签名校验（属于 `extern` 检查，单独 RFC）；
- 模糊测试 / 自动生成错误用例（后续单独开）。

---

## 8. 实施顺序与 commit 拆分建议

每个 commit 必须独立可编译、测试绿：

1. `feat(front): fail-fast gate between sema and codegen`（Phase 0）
2. `feat(sema): check local call arity`（3.2.1）
3. `feat(sema): check local call arg types`（3.2.2）
4. `feat(sema): check return type compatibility`（3.2.3）
5. `feat(sema): check struct field existence`（3.2.4）
6. `feat(sema): check assignment type compatibility`（3.2.5）
7. `feat(sema): error on same-scope redeclaration`（3.2.6）
8. `feat(sema): reject calling non-callable`（3.2.7）
9. `test(errors): add regression cases for 7 former leaks`（Phase 4）
10. `docs: document new frontend strictness checks`（CHANGELOG + 本方案 Accepted）

每个 commit 流程：

```
改 sema.go → go build → 跑 probe 用例 → run_tests.py --quick →
toolkit_build.py → commit → push github → 观察 gitee 同步
```

---

## 9. 参考

- 现状代码：
  - `compiler/internal/sema/sema.go`（6,118 行）
  - `compiler/internal/codegen/`（39,678 行）
  - `compiler/cmd/kaulac/main.go`（pipeline 主流程，~5,900 行）
- 现有文档：
  - `docs/semantic-analysis.md`
  - `docs/parser.md`
  - `docs/error-handling.md`
  - `docs/compiler-architecture.md`
- 实测 probe：`.probe/p1..p10.kl`（本次调研临时文件，合入前删除或迁移到
  `test/errors/`）

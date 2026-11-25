---
name: kaula-output-contract
description: "Use whenever an AI is about to write or output Kaula (.kl) code: test corpus, doc examples, snippets, or rewrites. Defines the mandatory self-check contract so generated .kl code compiles, formats round-trip stable, and matches Kaula idioms."
---

# Kaula 代码输出契约（AI 产出 .kl 前的强制自检）

AI 输出的每一段 `.kl` 代码，提交前逐条对照本契约。任何一条不满足都视为
「不合格产出」，必须返工。风格细节的完整规则见
[kaula-code-style.md](kaula-code-style.md)，本文是产出前的**检查清单**。

## 1. 语法硬规则（违反即编译失败或风格违规）

- [ ] 缩进 4 空格，无 Tab；行尾 LF；无分号结尾；每行 ≤ 120 字符。
- [ ] 大括号**同行**：`fn f() {`，不换行。
- [ ] 变量声明类型在后：`int count = 0`、`char* path = req.path`。
- [ ] `if`/`while` 条件带括号：`if (x > 0) {`。
- [ ] 注释只用 `//`，中文注释，不用 `/* */`。

## 2. 布尔与比较（Kaula 特有，最容易出错）

- [ ] 条件必须是布尔：禁止裸真值 `if (ptr)` → 写 `if (ptr != null)`。
- [ ] 禁止字面量作比较左值：`if (90 == x)` → `if (x == 90)`。
- [ ] 禁止 `if (x == true)` / `if (x == false)` → `if (x)` 的布尔变量直接用，
      取反写否定语义或 `!(...)`（比较结果才是布尔）。
- [ ] 布尔变量只赋 `true`/`false`；禁止 `int x = true`。
- [ ] 禁止 `as<int>(true)`、`as<bool>(x)` 之类的布尔转换。
- [ ] 指针比较只有 `==`/`!=`（可与 `null` 比）；字符串可直接 `==`/`!=`。

## 3. 命名

- [ ] 变量 camelCase（`docRoot`）；函数 snake_case（`handle_request`）；
      类型 PascalCase（`HttpResponse`）；常量 SCREAMING_SNAKE_CASE（`MAX_RETRIES`）；
      布尔变量 `is_`/`has_` 前缀；私有成员 `_` 前缀。

## 4. import

- [ ] import 置顶，std 优先按字母序，其余空行分隔。
- [ ] 本地模块 import 名 = 文件名：`import helper` → `helper.kl`；
      `import my.util` → `my/util.kl`。
- [ ] 路径导入带引号：`import "helper.kl"`；库导入目标目录须含 `kaula.json`。

## 5. 产出前自检流程

```bash
# ① 格式化并检查 round-trip 稳定（二次 fmt 与首次一致）
kaulac fmt file <file>.kl --write
kaulac fmt file <file>.kl > /tmp/second.txt && diff <file>.kl /tmp/second.txt

# ② 确认无 fmt 未覆盖节点
grep -n "unknown statement\|unknown expression" <file>.kl   # 必须为空

# ③ 编译验证（有环境时）
kaulac <file>.kl && ./a.out   # 或按 docs/tools/kaulac.md 的输出方式
```

**文档中的示例代码**额外要求：

- [ ] 能通过 `python scripts/verify_tutorials.py`（被包装成完整程序编译运行）。
- [ ] 片段末尾可用 `# ...` 行内注释标注预期输出，会被校验器保留比对。
- [ ] 片段要么自带 `fn main()`，要么只含可被包装进 main 的顶层声明+语句。

**test/ 语料**额外要求：

- [ ] 正向语料可编译可运行；负向语料按 testrunner 的 expect-fail 标注约定写明
      预期错误（参考 `test/errors/` 现有格式）。

## 6. 禁止项速查

| 禁止 | 改为 |
|------|------|
| `if (ptr)` | `if (ptr != null)` |
| `if (90 == x)` | `if (x == 90)` |
| `if (x == true)` | `if (x)`（布尔变量裸用） |
| `int x = true` | `bool x = true` 或重新设计类型 |
| `/* 注释 */` | `// 注释` |
| 行尾分号 | 删除分号 |
| Tab 缩进 | 4 空格 |
| `kaulac fmt` 后输出含 `/* unknown ... */` | 修复对应语句/表达式写法 |

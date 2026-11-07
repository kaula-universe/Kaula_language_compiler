# 跨语言 Token 效率基准（Kaula / Python / Go / C / C++ / Rust）

衡量"AI 写某种语言要烧多少 token"的**可复现口径**，用现成任务集 + 固定 tokenizer。

## 为什么需要三级

单一指标都会误导，因为成本由三件独立的事叠加：

| 级别 | 衡量 | 与模型关系 |
|---|---|---|
| **A 静态密度** | 同一段代码被 tokenizer 切成几个 token | 无关（只跟词表有关） |
| **B 任务归一产出量** | 实现**同一个功能**要写多少 token | 弱（跟语法冗余度有关） |
| **C 端到端成本** | 给定题面到测试通过共烧多少 token | 强（跟模型对该语言的先验有关） |

C 级在没有任何 API key 时可以用 **oracle 下界**（假设一次写对）来测：
`prompt_tokens + completion_tokens`，能把"题面成本"和"输出成本"分开。

## 任务集与语料（全部现成，不自己造题）

- 任务定义：**HumanEval**（OpenAI 原版，Python 题面 + 官方断言）
- 多语言题面：**MultiPL-E**（`nuprl/MultiPL-E`，`humaneval-{go,cpp,rs}` 配置）
- 抽取任务：`HumanEval/0, 2, 4, 23, 43, 65`（数组、浮点、字符串各覆盖到）
- C 与 Kaula 没有官方译文 → 用**同一段英文题面** + 本语言的骨架，只改签名写法

> 说明：MultiPL-E 只提供题面与测试，不提供参考解。因此六语言的参考实现均由本目录
> `make_solutions.py` 生成，并逐个编译运行验证（36/36 PASS）。Python 侧取自
> `openai_humaneval` 的 canonical solution。

## 口径定义

- **实现区**：`BENCH:BEGIN` … `BENCH:END` 之间的全部代码，含 `import` / `#include` /
  类型声明 / 辅助函数 —— 即"模型必须写出来的那部分"。
- **驱动区**：`BENCH:MAIN:BEGIN` … 之间的本地自检 `main`，**不计 token**（各语言测试
  脚手架差异太大，计进去就不可比）。
- Tokenizer：`o200k_base`（GPT-4o 系）为主，`cl100k_base` 做敏感性校验。

## 结果

### A 级：静态密度（六语言参考实现，o200k_base）

| 语言 | 字节 | tokens | B/token | tokens/行 |
|---|---|---|---|---|
| Rust | 2188 | 761 | 2.88 | 7.25 |
| Python | 2348 | 864 | 2.72 | 9.39 |
| Go | 2519 | 929 | 2.71 | 5.70 |
| C++ | 3021 | 974 | 3.10 | 7.11 |
| C | 3132 | 1100 | 2.85 | 7.24 |
| **Kaula** | 3449 | **1153** | 2.99 | 7.49 |

仓库自有语料（口径不同，只作背景）：

| 语料 | 文件 | 字节 | tokens | B/token |
|---|---|---|---|---|
| `test/*.kl` | 72 | 141992 | 44539 | 3.19 |
| `docs/tutorials/examples/*.kl` | 13 | 20774 | 6247 | 3.33 |
| `std` 的 C 运行时 | 74 | 1017088 | 298431 | 3.41 |

### B 级：任务归一产出量（实现区 tokens，o200k_base）

| 语言 | 实现区 tokens | 相对最小值 | 行数 | tokens/行 |
|---|---|---|---|---|
| Python | 201 | 1.00x | 27 | 7.44 |
| Rust | 322 | 1.60x | 40 | 8.05 |
| Go | 420 | 2.09x | 85 | 4.94 |
| C++ | 439 | 2.18x | 65 | 6.75 |
| **Kaula** | 509 | 2.53x | 74 | 6.88 |
| C | 521 | 2.59x | 75 | 6.95 |

### C 级：oracle 下界（题面 + 一次写对的实现）

| 语言 | prompt tokens | completion tokens | 合计 | 相对下界 |
|---|---|---|---|---|
| Python | 577 | 160 | 737 | 1.00x |
| Rust | 579 | 238 | 817 | 1.11x |
| Go | 646 | 289 | 935 | 1.27x |
| **Kaula** | 665 | 382 | 1047 | 1.42x |
| C | 679 | 380 | 1059 | 1.44x |
| C++ | 745 | 322 | 1067 | 1.45x |

## 结论

1. **表层密度上 Kaula 不差**：2.99 B/token，略低于 Python（2.72），高于 C（2.85）。
   大头不是语法，而是"自造标识符被子词化"（`dynobj_contains` = 3 token、
   `isDynamicObjectSlot` = 4 token，而 `println` = 1 token）。
2. **任务归一后差距明显**：Kaula 2.53x Python、与 C 基本持平。原因不是啰嗦，而是
   *Python 的 `sum()`、切片长度、`%` 都内建*，而 Kaula 需要显式 `for` 循环、
   显式 `as<i64>(...)` 转换、显式传数组长度。
3. **题面成本被低估的语言**：C/Kaula 在 C 级用同一段英文题面，prompt 反而比
   MultiPL-E 的官方译文短一点点；真实场景中 Kaula 还需要额外注入标准库 API 文档
   （`.kl` 层 `dynobj_*` / `string_*` 的可用名单），这部分没有计入下界表，需在 llm 模式里体现。
4. **真正决定 Kaula 成本的是 C 级的重试轮数**，不是 A/B 级。模型对 Kaula 的先验几乎为零，
   首轮正确率低 → tokens-to-green 会被成倍放大。这个数必须用 `--mode llm` 实测。

## 复现

```bash
cd bench/token_efficiency

python3 make_solutions.py        # 生成六语言参考实现
python3 normalize_markers.py     # 统一实现区/驱动区标记（幂等）
python3 measure_a.py             # A 级 → results/a_density.csv
python3 measure_b.py             # B 级 → results/b_normalized.{csv,md}
python3 measure_c.py             # C 级 oracle → results/c_oracle.csv
python3 measure_c.py --mode check    # 管道自检：oracle 代码必须能编译运行

# 需要 API key 才能测真实的 tokens-to-green
OPENAI_API_KEY=sk-... python3 measure_c.py --mode llm \
    --base-url https://api.openai.com/v1 --model gpt-4o-mini --langs py,kl
```

`measure_c.py --mode llm` 的行为：题面 → 生成 → 抽代码 → 编译 → 失败则把编译器输出
整段回灌 → 重试（默认 3 次），记录每轮 usage 与 `tokens_to_green` / `repairs`。

## 基准过程中发现的语言问题（如实记录，未全部修复）

1. **小写 `string` 是 `const char*`，大写 `String` 才是对象字符串**。`s.length()`
   两条路径都不可用：`const char*` 接收者生成 `string_object_length(const char*)` → C 类型错误；
   `String` 接收者生成 `Object_length` → **该符号在 std/ 中根本不存在** → 链接失败。
   当前唯一可用写法是 `std.string.length(s)`。已在 `solutions/kl/23_strlen.kl` 沿用。
2. **切片没有长度访问器**：`xs.length` 生成 `xs.length`（C 层不是结构体）→ 编译失败。
   因此 Kaula 侧数组型任务必须显式传 `n`，签名与 C 一样；这会真实增加产出 token。
3. **`fn strlen` 直接撞 libc 名字**：codegen 生成同名 C 函数与 `<string.h>` 冲突 →
   `conflicting types for 'strlen'`。实际写作需避开 C 标准库已占用的名字。
4. **`for i in 0..n` 不支持**：必须 `range(n)`；`for i in range(a, b)` 可用。
   这是语法约束，不是 bug，但会改变产出的 token 形状（`range(n)` vs `0..n` 一样长）。

> 第 1、2 条属于可修缺陷（`exprgen.go` 的 `length` 分支与切片长度语义），
> 但涉及 sema/codegen 两侧语义决定，未在本次基准改动中夹带修改，避免把未验证的
> 语义变更混进提交。

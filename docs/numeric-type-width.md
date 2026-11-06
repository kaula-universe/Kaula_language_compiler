# 数字类型位宽口径（审计 §1.3 统一后）

本文件是 `int` / `uint` / 整数字面量位宽的**权威规范**。历史上这三处在 sema、
codegen、类型表里各自表述、互相矛盾（见 `security-review-2026-10.md` §1.3），
`231dea0` 起按下表统一。改任何一处都必须同步另外两处，否则就是 ABI 级静默错。

## 统一口径

| Kaula 类型 | C 映射 | `GetTypeSize` | `numeric_names.go` 规范拼写 |
|-----------|--------|---------------|------------------------------|
| `int`     | `int32_t` | 4 | `i32`（`int` 属被禁拼写） |
| `uint`    | `uint32_t` | 4 | `u32` |
| `i8/i16/i32/i64` | `int8_t/int16_t/int32_t/int64_t` | 1/2/4/8 | 同名 |
| `int8/16/32/64` | 同上 | 1/2/4/8 | 被禁拼写，规范为 `i8..i64` |

**整数字面量的默认类型是 `i64`，不是 `int`。**

理由：字面量若推成 `int`，在新口径下等于 `int32_t`，会把一个无显式类型的
`auto x = 3000000000` 凭空窄化到 32 位并静默溢出；推成 `i64` 才与
"字面量不按数值大小收窄"的既有修复一致。需要窄类型时必须显式写 `i32` / `u32`。

## 三处必须同时改的实现点

| 位置 | 内容 |
|------|------|
| `compiler/internal/codegen/typegen.go` → `globalTypeMap` | Kaula 类型名 → C 类型 |
| `compiler/internal/codegen/typegen.go` → `GetTypeSize` 的 `basicTypeSizes` | 位宽（字节） |
| `compiler/internal/sema/numeric_names.go` | 被禁拼写 → 规范拼写映射 |

另有 `sema.go` 的 `inferExpressionType` / `inferLiteralType`（字面量默认类型）
与 `promoteIntegerType` 的精度序（`int`/`i32` 同档，`i64` 高一档，`u64` 最高）。

## 回归门禁

- `compiler/internal/codegen/func_pointer_test.go` → `TestParseVoidSignatureFuncPointer`：
  `void(int,int)int` 必须是 `int32_t (*)(int32_t, int32_t)`，`void()int` 必须是
  `int32_t (*)(void)`；无返回段的 `void(int)` 仍是数据指针 `void*`。
- `compiler/internal/sema/multi_decl_test.go` → `TestMultiVarDeclInFunction`：
  `auto p = 1, q = "text"` 中 `p` 必须是 `i64`、`q` 是 `string`。

## 仍未解决的第 4 处（遗留待办）

§1.3 列的第 4 条**尚未处理**：pkgblib 的 C 头按 `.kl` 默认按 LP64 生成，
在 LLP64 平台（Windows）上会静默改变所有 `long` / `size_t` / `uintptr_t` 宽度
—— `stdlib/header_gen.go` 与 `builder.go` 的 `clangArgs` 里没有传 `--target`。
本文件只固定了 `int`/`uint` 的口径，不覆盖该问题；触碰到 pkgblib 头生成时须一并处理。

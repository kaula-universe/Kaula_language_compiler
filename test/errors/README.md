# 负向回归用例（Frontend Strictness）

这些 .kl 文件**故意写错**，用于验证前端（sema）能在 codegen 之前拦下错误。
每个文件首行带 `// kaulac:expect-fail <错误码>` 标注：`kaulac test` 的
runner 看到标注后要求**编译失败**，且编译器输出包含期望错误码——失败即
PASS，编译成功或错误码不符都是 FAIL（防止"失败原因变了也通过"）。

## 验证方法

```bash
# 一条命令跑全部负向语料（含 expect-fail 标注的 PASS/FAIL 判定）
kaulac test test/errors
```

## 用例清单

| 文件 | 期望错误码（expect-fail 标注） | 场景 |
|---|---|---|
| p1_arg_count.kl | wrong_arg_count_local | 本地函数实参数量 ≠ 形参 |
| p2_arg_type.kl | arg_type_mismatch | 实参类型不兼容形参 |
| p4_wrong_field.kl | no_such_field | 访问 struct 不存在字段 |
| p5_assign_mismatch.kl | assign_type_mismatch | 赋值左右类型不兼容 |
| p6_return_type.kl | return_type_mismatch | return 表达式类型 ≠ 声明返回类型 |
| p6b_return_void.kl | return_value_in_void_fn | void 函数 return 带值 |
| p7_redecl.kl | redeclaration | 同作用域重复声明（错误） |
| p8_call_nonfn.kl | calling_non_callable | 调用非函数标识符 |
| p9_missing_return.kl | missing_return | 非 void 函数存在不经过 return 的路径 |

> 兄弟控制流块里各自声明同名变量是**合法**写法（仅警告），正向回归见
> `test/redecl_sibling_blocks.kl`。

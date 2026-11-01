# 负向回归用例（Frontend Strictness）

这些 .kl 文件**故意写错**，用于验证前端（sema）能在 codegen 之前拦下错误。
它们不被 `scripts/run_tests.py` 收集（那个脚本期望全部 PASS）。

## 验证方法

```bash
# 期望：每个文件都在前端报错（exit != 0），且输出包含指定错误码
for f in test/errors/*.kl; do
  echo "=== $f ==="
  build/bin/kaulac "$f" 2>&1 | grep -E "语义错误|semantic error" | head -3
done
```

## 用例清单

| 文件 | 期望错误码 | 场景 |
|---|---|---|
| p1_arg_count.kl | wrong_arg_count_local | 本地函数实参数量 ≠ 形参 |
| p2_arg_type.kl | arg_type_mismatch | 实参类型不兼容形参 |
| p4_wrong_field.kl | no_such_field | 访问 struct 不存在字段 |
| p5_assign_mismatch.kl | assign_type_mismatch | 赋值左右类型不兼容 |
| p6_return_type.kl | return_type_mismatch | return 表达式类型 ≠ 声明返回类型 |
| p6b_return_void.kl | return_value_in_void_fn | void 函数 return 带值 |
| p7_redecl.kl | redeclaration | 同作用域重复声明 |
| p8_call_nonfn.kl | calling_non_callable | 调用非函数标识符 |

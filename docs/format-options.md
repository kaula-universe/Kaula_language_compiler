# 数值格式化选项

`std/format/format.h` 提供 C 缓冲区接口和 KMM 管理的字符串接口。
`format_int_opts`、`format_uint_opts`、`format_float_opts` 接受 `FormatOptions*`；
传入 NULL 时使用默认格式（浮点小数位数为 6）。

| 选项 | 行为 |
| --- | --- |
| width | 最小字节宽度；小于等于 0 时不填充，不限制数值长度 |
| left_align | 左对齐；默认右对齐 |
| pad_char | 自定义单字节填充；0 表示空格 |
| zero_pad | 右对齐时在符号和进制前缀之后补零，优先于 pad_char；左对齐时忽略 |
| show_sign | 非负值显示 `+`；负数和负零保留 `-` |
| show_base | 二、八、十六进制使用 `0b`、`0o`、`0x`，包含零值；其他进制无前缀 |
| uppercase | 数字字母、前缀、指数和 INF/NAN 大写 |
| thousands | 十进制整数部分每三位加逗号，不分组小数、指数或非十进制整数 |
| scientific | 浮点使用 e/E 科学计数法，整数忽略 |
| precision | 浮点小数位数，负值使用 6，大于 20 时限制为 20；整数忽略 |

分组先于宽度填充，填充零不参与分组。宽度包含符号、前缀和分隔符。
浮点舍入与小数点采用 C 运行时规则及当前 locale；默认 C locale 使用点号。
基础 `format_uint` 保留历史的大写行为，`format_uint_opts(..., NULL)` 默认小写。
基础 `format_float` 保留负 precision 返回 -1 的约定。

缓冲区接口返回实际写入字节数（不含 NUL），截断始终保留完整字段的左侧前缀。
有效缓冲区始终以 NUL 结束；容量为 1 时返回空串。NULL 或容量为 0 返回 -1；
整数进制超出 2..36 返回 -1 并清空有效缓冲区。
浮点临时空间依据 DBL_MAX_10_EXP 分配，支持 DBL_MAX 固定小数和分组输出。

## Kaula 友好的接口

新增声明采用 i32/i64/u64/f64 数值类型。选项对象无需从 Kaula 操作 C 结构体字段：

- `format_options_create()`：KMM 分配，默认 precision=6。
- `format_options_set_layout(options, width, left_align, zero_pad, pad_char)`：pad_char 是 i32 ASCII 编码，1..127 之外用空格。
- `format_options_set_precision(options, precision)`。
- `format_options_set_flags(options, show_sign, show_base, uppercase, thousands, scientific)`。
- `format_int_text(value, base, options)`、`format_uint_text(value, base, options)`、`format_float_text(value, options)`：返回完整的 KMM 字符串，分配或参数失败返回 NULL。

对象和字符串遵循 KMM 作用域生命周期，不可用 libc free。宽度越大，text 接口申请的空间越大；
对不可信宽度应由调用者限制。setter 接受 NULL 并无操作。

C 示例（同一组函数可由 Kaula FFI 调用）：

```c
FormatOptions* o = format_options_create();
format_options_set_layout(o, 12, false, true, 32);
format_options_set_flags(o, true, true, true, false, false);
char* text = format_uint_text(255, 16, o); /* +0X0000000FF */
```

声明遵循 `compiler/internal/stdlib/analyzer.go` 的 Clang FunctionDecl 提取规则。
本改动不修改生成的 stdlib.json。现有编译器从 stdlib.json 加载标准库函数清单，
旧清单尚未登记这些函数时，需要通过项目的接口登记/生成流程更新，或使用显式 extern fn；
单独更新头文件不会自动让旧清单拥有新函数。头文件测试验证真实 Clang AST 中的参数类型，
不声称完成整套 Kaula 编译器集成测试。

## 验证

在仓库根目录，使用已构建的真实 KMM runtime：

```sh
clang -std=c11 -Wall -Wextra -Werror -Wno-unused-parameter tests/format_options_test.c std/format/format.c build/lib/kaula_runtime.lib -o build/format_options_test.exe
build/format_options_test.exe
clang -std=c11 -Wall -Wextra -Werror -Wno-unused-parameter -fsanitize=undefined tests/format_options_test.c std/format/format.c build/lib/kaula_runtime.lib -o build/format_options_ubsan.exe
build/format_options_ubsan.exe
python tests/format_options_header_test.py
```

C 测试无分配器替身，覆盖 2..36 进制、符号/前缀/填充的交互、逐容量截断及保护字节、
INT64_MIN、UINT64_MAX、DBL_MAX、负零、无穷大、NaN、科学记数、构建器大浮点和 KMM 字符串接口。

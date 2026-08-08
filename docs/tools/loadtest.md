# loadtest — 语义分析负载测试工具

`loadtest` 是开发期验证工具：对 `.kl` 源文件执行词法→语法→语义全流程分析，输出解析错误数、语句数及语义错误，用于验证语义分析器行为。

保存位置：`kaula-compiler/cmd/loadtest/`（未纳入 `build/bin`，需手动构建）。

## 用法

```
用法: loadtest <file.kl> <stdlib.json路径>
```

## 参数

| 参数 | 说明 |
|------|------|
| `<file.kl>` | 要分析的 Kaula 源文件 |
| `<stdlib.json>` | 标准库配置路径（如 `build/bin/stdlib.json` 或仓库内 `kaula-compiler/stdlib.json`） |

## 输出格式

```
loaded 58 modules
parse errors: 0, stmts: 12
SEM: 5:8 类型不匹配: ...
NO SEMANTIC ERRORS
```

- `loaded N modules`：标准库配置加载的模块数
- `parse errors`：语法错误数量；`stmts`：顶层语句数
- 每条语义错误打印 `SEM: <行>:<列> <消息>`
- 无语义错误时输出 `NO SEMANTIC ERRORS`

## 构建

```bash
cd kaula-compiler/cmd/loadtest
go build -o loadtest.exe
```

## 示例

```bash
# 用仓库内标准库配置验证一个源文件
loadtest test.kl kaula-compiler/stdlib.json

# 用构建产物的标准库配置
loadtest test.kl build/bin/stdlib.json
```

## 相关文档

- 工具索引：[README.md](README.md)
- 语义分析器：[semantic-analysis.md](../semantic-analysis.md)
- 标准库配置：[stdlib-integration.md](../stdlib-integration.md)
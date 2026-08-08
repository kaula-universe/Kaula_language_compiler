# dumpast — AST 转储工具

`dumpast` 是开发期诊断工具：对 `.kl` 源文件执行词法+语法分析，把 AST 以树形文本打印到标准输出，便于调试解析器或观察语法结构。

保存位置：`kaula-compiler/cmd/dumpast/`（未纳入 `build/bin`，需手动构建）。

## 用法

```
用法: dumpast <file.kl>
```

## 参数与行为

| 参数 | 说明 |
|------|------|
| `<file.kl>` | 要分析的 Kaula 源文件 |

- 无参数时打印 `usage: dumper file.kl` 并退出（程序内部名 `dumper`）
- 文件读取失败打印 `read: <错误>`
- 输出开头为 `parse errors: true/false`
- 顶层语句逐条打印类型，再递归打印嵌套语句（缩进 2 空格）

## 输出格式

```
parse errors: false
[0] *ast.ImportStatement
[1] *ast.FunctionStatement
  [0] *ast.VariableDeclaration
    Name="x" Type="i32" IsAuto=false IsConst=false
  [1] *ast.IfStatement
    [0] *ast.ExpressionStatement
      Expression=*ast.BinaryExpression
```

## 构建

```bash
cd kaula-compiler/cmd/dumpast
go build -o dumpast.exe
```

## 相关文档

- 工具索引：[README.md](README.md)
- AST 节点定义：[ast.md](../ast.md)
- 语法解析器：[parser.md](../parser.md)
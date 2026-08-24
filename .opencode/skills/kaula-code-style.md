---
name: kaula-code-style
description: "Use when writing or reviewing Kaula (.kl) source code. Apply these rules for formatting, naming, and conventions."
---

# Kaula 代码规范 (Code Style)

本文档定义 Kaula 语言 (`.kl`) 代码的风格规范。格式化以 `kaulafmt` 输出为准,
手工书写时遵循以下规则。

## 1. 基础格式

- 缩进 **4 空格**,禁止 Tab。
- 行尾 LF,文件末尾保留一个换行。
- 每行不超过 120 字符。
- 无分号结尾。

## 2. 大括号

Kaula 大括号**同行**(与 C 的 astyle Java 风格不同,这是 Kaula 自身风格):

```kaula
fn handle_request(req: HttpRequest*, res: HttpResponse*) {
    if (path == "/") {
        http_response_set_status(res, 200)
    } else if (path == "/hello") {
        http_response_set_status(res, 201)
    }
}
```

- 空声明: `class EmptyClass {}`、`struct EmptyStruct {}`、`enum EmptyEnum {}`、`fn f() {}`。
- 含语句的函数体必须展开,不省略大括号。

## 3. 条件与比较

- 条件语句**推荐带括号包裹条件**:
  ```kaula
  if (x > 0) {
      println("positive")
  }
  while (i < 10) {
      i = i + 1
  }
  ```
  > 语法上 `if x > 0 {`(无括号)也合法,但新代码一律使用带括号形式。

- 条件必须是**布尔值**:布尔变量裸名、`true`/`false`、或比较表达式。
  - 禁止: 非布尔变量裸名 (`if (ptr)`) → 改写为 `if (ptr != null)`
  - 禁止: 数字字面量裸名 (`if (1)`)
  - 禁止: 字面量作比较左值 (`if (true == x)`、`if (90 == x)`)
  - 禁止: 比较右侧使用 true/false (`if (x == true)`)

- 比较规则:
  - 数值: `==` `!=` `<` `>` `<=` `>=`
  - 指针: 仅 `==` `!=`(可与 `null` 比较): `if (server == null)`
  - 字符串: `==` `!=`(`char*` 与 `string` 可直接比较): `if (path == "/")`
  - 布尔转换禁止: 不能 `as<int>(true)` 或 `as<bool>(x)`;布尔值只能来自
    布尔字面量、布尔变量或比较表达式。

## 4. 变量与声明

- 变量声明 `name: Type` 形式(类型在后):
  ```kaula
  fn handle(req: HttpRequest*, res: HttpResponse*) {
      int count = 0
      char* path = req.path
      int httpCode = 0
  }
  ```
- 布尔变量只能赋 true/false,`int x = true` 禁止。
- 字段声明支持 `const` 前缀: `name: const char*`。

## 5. 命名约定

| 类别 | 规则 | 示例 |
|------|------|------|
| 变量 | 小驼峰 (camelCase) | `docRoot`、`httpCode` |
| 函数 | 全小写下划线分隔 (snake_case) | `handle_request`、`http_response_set_status` |
| 结构体/类/枚举 | 大驼峰 (PascalCase) | `HttpResponse`、`Color`、`Vec2` |
| 常量 | 全大写下划线分隔 (SCREAMING_SNAKE_CASE) | `MAX_RETRIES`、`PI` |
| 私有成员 | 前面加下划线 `_` | `_internal`、`_cache` |
| 布尔变量 | is_/has_ 前缀 | `is_valid`、`has_error` |

## 6. import

- import 置于文件顶部,std 库优先按字母序,其他 import 空行分隔(kaulafmt 自动处理)。
- 本地模块 import 名 = 文件名:
  - `import helper` → 文件 `helper.kl`
  - `import my.util` → 文件 `my/util.kl`(点号转目录),或兼容 `my.util.kl`
- 路径导入(Python 风格,支持相对/绝对路径),用字符串引号:
  - `import "helper.kl"` → 单文件导入,无额外要求
  - `import "mylib"` → 库导入:目标目录必须含 `kaula.json`(可为空文件),导入该目录下全部 `.kl` 文件
  - 相对路径以当前文件所在目录为基准,其次回退到工作目录

## 7. 注释

- 中文注释,`//` 行注释为主。
- 函数上方用 `// 说明` 注释;复杂逻辑解释"为什么"。
- 不使用 `/* */` 块注释。

## 8. 提交前检查

```bash
kaulafmt -w <file>.kl
```

- 格式化输出不得包含 `/* unknown statement */` 或 `/* unknown expression */` 注释
  (表示 kaulafmt 未覆盖的节点,需要修复)。
- 二次运行 kaulafmt 结果应与首次一致(round-trip 稳定)。

# 代码生成器 (Codegen)

代码生成器将 AST 转换为 C 源代码。实现位于 `internal/codegen/` 目录。

## 设计

- **模块化架构**：类型/函数/表达式/语句生成器分离
- **模板驱动**：使用 Go 模板生成 C 代码
- **作用域管理**：符号表支持嵌套作用域
- **SOR 集成**：支持子所有权释放分析结果
- **泛型实例化缓存**：避免重复生成泛型代码
- **源码映射**：生成调试用的源码映射

## 目录结构

```
codegen/
├── codegen.go         # 核心代码生成器
├── generator.go       # 生成器接口定义
├── typegen.go         # 类型生成
├── funcgen.go         # 函数生成
├── exprgen.go         # 表达式生成
├── stmtgen.go         # 语句生成
├── template.go        # 模板管理
├── plugin.go          # 插件系统
├── sor_codegen.go     # SOR 集成
├── sourcemap.go       # 源码映射
└── cache/             # 生成缓存
```

## 代码生成流程

```go
// CodeGenerator.Generate 方法
func (cg *CodeGenerator) Generate(program *ast.Program) string {
    // 1. 排序 AST 语句到四个输出区域
    //    - 类型定义
    //    - 全局变量
    //    - 函数声明
    //    - main 代码
    
    // 2. 解析导入/包含
    //    - 从 stdlib.json 加载模块头文件
    //    - 解析第三方库依赖
    
    // 3. 生成前向声明
    //    - 公开函数的前向声明
    //    - 泛型实例化函数
    
    // 4. 生成 C 代码
    //    - 使用模板或直接拼接
    //    - 生成源码映射
    
    // 5. 返回完整 C 源代码
}
```

## 类型生成 (typegen.go)

将 Kaula 类型映射为 C 类型：

```
Kaula 类型 → C 类型
int        → i64
float      → f32
double     → f64
bool       → bool
char       → char
string     → const char*
void       → void
Type*      → Type*
Box<int>   → Box_int (泛型实例化)
```

## 函数生成 (funcgen.go)

生成 C 函数：

```c
// Kaula 源码
fn add(int a, int b) int {
    return a + b
}

// 生成的 C 代码
i64 add(i64 a, i64 b) {
    return a + b;
}
```

函数生成处理：
- 参数类型映射
- 返回类型映射
- 函数体语句生成
- KMM 作用域管理
- 泛型实例化

## 表达式生成 (exprgen.go)

生成 C 表达式：

```
Kaula 表达式 → C 表达式
a + b        → (a + b)
a == b       → (a == b)
!a           → (!a)
&a           → (&a)
obj.field    → (obj->field)
arr[i]       → (arr[i])
func(a, b)   → func(a, b)
(i64)(x)     → ((i64)(x))
```

## 语句生成 (stmtgen.go)

生成 C 语句：

```
Kaula 语句 → C 语句
if (cond) {}     → if (cond) {}
while (cond) {}  → while (cond) {}
for (;;) {}      → for (;;) {}
return x         → return x;
break            → break;
continue         → continue;
```

## 模板管理 (template.go)

使用 Go 模板生成 C 代码框架：

```go
// templates/main.c.tmpl
#include <stdio.h>
#include <stdlib.h>

// 类型定义
{{.TypeDefinitions}}

// 全局变量
{{.GlobalVariables}}

// 函数声明
{{.FunctionDeclarations}}

// 函数实现
{{.FunctionImplementations}}

// main 函数
int main(int argc, char* argv[]) {
    {{.MainBody}}
    return 0;
}
```

## 插件系统 (plugin.go)

支持注册自定义代码生成插件：

```go
// 注册插件
cg.RegisterPlugin("myPlugin", myPluginHandler)

// 插件接口
type Plugin interface {
    Generate(node ast.Node) string
    CanHandle(node ast.Node) bool
}
```

## SOR 集成 (sor_codegen.go)

将 SOR 分析结果集成到代码生成：

- 生成 KMM 作用域清理代码
- 插入所有权检查
- 生成释放点代码
- 池容量定义（`KMM_V4_POOL_SIZE`）

## 源码映射 (sourcemap.go)

生成 C 代码到 Kaula 源码的映射：

```go
type SourceMap struct {
    CLine    int  // C 代码行号
    KLLine   int  // Kaula 源码行号
    KLColumn int  // Kaula 源码列号
    KLFile   string // Kaula 源文件
}
```

## 作用域管理

```go
// 进入新作用域
cg.EnterScope("function_name")

// 添加符号
cg.AddSymbol("x", "int", false)

// 查找符号
symbol := cg.GetSymbol("x")

// 退出作用域
cg.ExitScope()
```

## 泛型实例化

```go
// 实例化泛型函数
code, err := cg.InstantiateGeneric("max", []string{"int"}, line)

// 泛型缓存
type GenericInstanceCache struct {
    instances map[string]string  // key: "funcName<typeArgs>"
}
```

## KMM 内存管理

代码生成器跟踪 KMM 作用域深度：

```go
// KMM 作用域深度
cg.kmmScopeDepth++

// 生成作用域开始/结束代码
// kmm_scope_begin()
// ... 函数体 ...
// kmm_scope_end()
```

## API

```go
// 创建代码生成器
cg := NewCodeGenerator(config)

// 生成代码
cCode := cg.Generate(program)

// 设置 SOR 结果
cg.SetSORResult(sorResult)

// 设置标准库配置
cg.SetStdlibConfig(stdlibConfig)

// 设置源文件
cg.SetSourceFile("main.kl")
```

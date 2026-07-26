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

### 用户类型命名消歧

为避免与系统头文件中的宏/类型冲突（如 Windows `wingdi.h` 的 `Rectangle` 函数宏），所有用户定义的 struct/class/enum/interface 在生成 C 代码时统一添加 `K_` 前缀：

```
Kaula 类型 → C 类型
struct Point        → typedef struct K_Point { ... } K_Point
struct Rectangle    → typedef struct K_Rectangle { ... } K_Rectangle
class Box           → typedef struct K_Box { ... } K_Box
interface Drawable  → typedef struct K_Drawable_MethodGroup { ... } K_Drawable_MethodGroup
enum Option         → typedef enum { Option_Kind_... } Option_Kind
                       typedef struct K_Option { Option_Kind kind; union { ... } data; } K_Option
```

`MapKaulaTypeToC` 函数自动处理前缀转换：当类型名注册在 `structTypes` 中时，返回带 `K_` 前缀的 C 类型名。用户代码中直接使用 Kaula 原名（如 `Rectangle`），编译器在代码生成阶段统一替换为 `K_Rectangle`。

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
if (cond) {}                       → if (cond) {}
while (cond) {}                    → while (cond) {}
for x in range(N) { body }        → for (long long _i_ = 0; _i_ < (N); _i_ += 1) { long long x = _i_; body }
for x in range(s, e) { body }     → for (long long _i_ = (s); _i_ < (e); _i_ += 1) { long long x = _i_; body }
for x in range(s, e, step) { }    → for (long long _i_ = (s); _i_ < (e); _i_ += (step)) { long long x = _i_; ... }
                                     // step < 0 时条件自动改为 _i_ > (e)
for x in arr { body }             → for (size_t _i_ = 0; _i_ < (arr).len; _i_++) { T x = arr.ptr[_i_]; body }
return x                          → return x;
break                             → break;
continue                          → continue;
```

注：Kaula 不再支持 C 风格 `for(init; cond; update)`。所有计数循环通过 `range(...)` 表达，索引变量 `_i_` 由编译器管理，用户代码不可见。

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

## KMM V4 内存管理

KMM V4 是 Kaula 的默认内存分配器，代码生成器负责将 Kaula 层的内存操作转换为高效的 C 代码。

### 设计架构

KMM V4 采用 **per-thread heap + bump allocation + scope-based reclamation** 架构：

- **Per-Thread Heap**：每个线程从全局池批量获取内存块，分配时只推进线程本地 offset
- **Bump Allocation**：分配即指针推进，O(1) 复杂度
- **Scope-based Reclamation**：作用域退出时批量回收，`kmm_v4_free` 为 no-op

### 代码生成的三大优化

#### 1. std_malloc inline 重写

编译器将所有 `std_malloc` 调用无条件重写为 `kmm_v4_alloc_auto` inline 调用：

```kaula
// Kaula 源码
auto buf = std.memory.std_malloc(1024)
```

```c
// 生成的 C 代码（inline 展开）
void* buf = kmm_v4_alloc_auto(1024);
```

**关键规则**：
- 当 KMM 启用时，`std_malloc` → `kmm_v4_alloc_auto` 是无条件重写
- `kmm_v4_alloc_auto` 是 `static inline` 函数，编译器可完全内联
- 消除函数调用开销，分配路径仅剩指针推进 + 比较

#### 2. 作用域自动插入

代码生成器跟踪 KMM 作用域深度，在函数入口/出口自动插入 scope 操作：

```kaula
// Kaula 源码
fn process() {
    auto a = std.memory.std_malloc(64)
    auto b = std.memory.std_malloc(128)
    // ... 使用 a, b ...
}
```

```c
// 生成的 C 代码
void process(void) {
    kmm_v4_scope_push();           // 函数入口：保存 thread heap offset
    void* a = kmm_v4_alloc_auto(64);
    void* b = kmm_v4_alloc_auto(128);
    // ... 使用 a, b ...
    kmm_v4_scope_pop();            // 函数出口：恢复 offset，批量回收
}
```

**作用域插入策略**：
- `kmm_v4_scope_push()` 插入在函数入口（prologue 之后）
- `kmm_v4_scope_pop()` 插入在所有退出路径（return/break/continue + 函数末尾）
- 嵌套作用域支持最大深度 64 层（`KMM_V4_MAX_SCOPE_DEPTH`）

```go
// 代码生成器中的 KMM 作用域管理
cg.kmmScopeDepth++

// 生成作用域开始代码
cg.emit("kmm_v4_scope_push();")

// 生成函数体
cg.generateBody(func.Body)

// 在所有退出路径插入 scope_pop
for _, exitPoint := range func.ExitPoints {
    cg.emitAt(exitPoint, "kmm_v4_scope_pop();")
}
```

#### 3. free 消除

KMM V4 中 `kmm_v4_free` 是 no-op，代码生成器不生成任何 free 调用：

```kaula
// Kaula 源码（如果用户写了 free）
std.memory.kmm_v4_free(buf)
```

```c
// 生成的 C 代码
// (无代码生成，kmm_v4_free 是空操作)
```

### 分配路径生成

代码生成器为不同的分配模式生成最优代码：

```kaula
// 1. 普通分配
auto p = std.memory.std_malloc(64)
// → kmm_v4_alloc_auto(64)

// 2. 零初始化分配
auto arr = std.memory.kmm_v4_calloc(100, sizeof_int)
// → kmm_v4_calloc(100, sizeof(int))

// 3. 字符串复制
auto s = std.memory.kmm_v4_strdup("hello")
// → kmm_v4_strdup("hello")
```

### #[no_kmm] 属性

使用 `#[no_kmm]` 属性可以在特定函数中禁用 KMM，回退到系统 malloc：

```kaula
#[no_kmm]
fn raw_buffer() void* {
    // 此函数内不插入 scope_push/pop
    // std_malloc 不会被重写为 kmm_v4_alloc_auto
    return std.memory.std_malloc(1024)
}
```

### SOR 模式下的集成

启用 `--sor` 时，SOR 分析结果指导代码生成：

```go
// SOR 分析传递池容量给代码生成
if config.SOR {
    sorResult := sor.AnalyzeFull(program)
    codegen.SetSORResult(sorResult)
    
    // 生成 KMM 池大小定义
    // #define KMM_V4_POOL_SIZE <poolSize>
}
```

SOR 模式下的额外优化：
- **逃逸分析**：无逃逸对象可省略 scope_push/pop
- **活跃性分析**：精确计算作用域释放点
- **池容量优化**：根据对象大小总和自动计算 `KMM_V4_POOL_SIZE`

### 性能数据

KMM V4 的 inline 机制带来显著性能提升（10M 次迭代基准测试）：

| 场景 | KMM V4 (inline) | malloc/free | 加速比 |
|------|-----------------|-------------|--------|
| 纯分配吞吐量（64B, 1M 次） | 3.5 ms | 72.7 ms | **20.5x** |
| 16B 小对象分配+回收 | 51.8 ms | 634.3 ms | **12.2x** |
| 64B 对象分配+回收 | 65.9 ms | 661.5 ms | **10.0x** |
| 混合负载（16~1024B） | 287.9 ms | 658.1 ms | **2.2x** |

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

# 标准库集成

编译器通过 `stdlib.json` 管理标准库函数签名，支持自动发现第三方库。实现位于 `internal/stdlib/` 目录。

当前标准库包含 **62 个模块**，超过 **800+** 个函数，分为标准库和 freestanding 无依赖标准库两大体系：

| 体系 | 模块数 | 特点 |
|------|--------|------|
| **std (标准库)** | 57 | 完整功能，依赖 libc，用户态应用首选 |
| **freestanding (无依赖库)** | 5 | 零依赖，弱符号，裸机/内核/嵌入式首选 |

## 设计

- **配置驱动**：通过 JSON 配置定义函数签名
- **自动发现**：自动扫描 `pkglib/` 目录加载第三方库
- **自动分析**：使用 Clang 解析 C 头文件生成配置
- **线程安全**：配置缓存支持并发访问

## 目录结构

```
stdlib/
├── stdlib.go      # 标准库配置加载
└── analyzer.go    # C 头文件自动分析
```

## stdlib.json 结构

```json
{
  "std.io": {
    "header": "std/io/io.h",
    "println": {
      "args": ["const char*"],
      "varargs": true
    },
    "print_int": {
      "args": ["i64"]
    },
    "file_open": {
      "args": ["const char*", "const char*"],
      "return": "File"
    }
  },
  "std.math": {
    "header": "std/math/math.h",
    "math_sqrt": {
      "args": ["f64"],
      "return": "f64"
    },
    "math_pow": {
      "args": ["f64", "f64"],
      "return": "f64"
    }
  }
}
```

## 函数签名格式

```go
type Function struct {
    Args     []string  // 参数类型列表
    Return   string    // 返回类型（可选）
    Varargs  bool      // 是否可变参数
    Header   string    // 头文件路径（模块级）
}
```

## 模块结构

标准库分为两个独立体系，编译器根据编译模式自动选择：

### std 标准库（托管模式默认）

```go
type Module struct {
    Name      string              // 模块名，如 "std.io"
    Header    string              // 头文件路径
    Functions map[string]*Function // 函数映射
}
```

模块示例：`std.io`、`std.math`、`std.memory`、`std.string` 等 57 个模块。

### freestanding 无依赖标准库（`--freestanding` 模式）

```json
// freestanding/dependencies.json
{
  "freestanding.base": [],
  "freestanding.memory": ["freestanding.base"],
  "freestanding.string": ["freestanding.base"],
  "freestanding.math": ["freestanding.base"],
  "freestanding.io": ["freestanding.base", "freestanding.string"]
}
```

模块列表：
| 模块 | 头文件 | 依赖 | 说明 |
|------|--------|------|------|
| `freestanding.base` | `freestanding/base/types.h` | - | 类型定义、`FS_WEAK` 宏 |
| `freestanding.memory` | `freestanding/memory/memory.h` | base | BSS 静态池分配器、内存操作 |
| `freestanding.string` | `freestanding/string/string.h` | base | 字符串处理、格式化转换 |
| `freestanding.math` | `freestanding/math/math.h` | base | 整数数学工具 |
| `freestanding.io` | `freestanding/io/io.h` | base, string | 格式化输出、弱钩子 `fs_output_putchar` |

**关键差异**：
- 所有 freestanding 函数在 C 层标记为弱符号（`FS_WEAK`）
- 不依赖 `<stdio.h>`/`<stdlib.h>`/`<string.h>`，仅用 `<stdint.h>`/`<stddef.h>`/`<stdbool.h>`
- `stdlib.json` 中 freestanding 模块的函数签名与 std 同名函数保持一致（如 `memset`/`strlen`/`print`/`println`）
- 编译器在 `--freestanding` 模式下自动链接 `kaula_freestanding.lib`/`libkaula_freestanding.a`

## 配置加载

### 加载标准库配置

```go
// 加载 stdlib.json
config, err := stdlib.LoadStdlibConfig("stdlib.json")

// 获取模块
module := config.GetModule("std.io")

// 获取函数
fn := config.GetFunction("std.io", "println")

// 检查是否是标准库函数
isStdlib := config.IsStdlibFunction("println")
```

### 函数查找

```go
// 获取 C 函数名
cName := config.GetCFunctionName("println")
// 返回: "println"

// 获取函数签名
fn := config.GetFunction("std.io", "println")
// 返回: &Function{Args: ["const char*"], Varargs: true}
```

## 第三方库集成

### 放库即用（drop-in）

第三方库**无需任何配置**：把源码目录放进 `pkglib/` 即可直接 `import` 使用。
编译器在加载配置时自动完成全部工作：

```
pkglib/
├── stb_image/        # 纯 C：头 + 可选源码
│   ├── stb_image.h
│   └── stb_image.c
├── nuklear/
│   └── nuklear.h
└── imgui/            # C++：自动生成 extern "C" 桥接
    ├── imgui.h
    ├── imgui.cpp
    └── imgui_kbridge.h/.cpp   # 自动生成，勿手改
```

首次 `import imgui` 时编译器依次自动完成：

1. **自动分析**：无 `<库名>.json` → 用 Clang 提取函数签名并写配置；配置已存在但过期（自动生成配置 + 头/源码比配置新，或配置里登记的头文件已消失）→ 自动重新分析
2. **自动桥接**（C++ 头文件）：生成 `<lib>_kbridge.h/.cpp`（`extern "C"`），并经 `clang -fsyntax-only` 编译自检，失败则回退并跳过该头
3. **自动构建**：归档缺失或源码更新时，编译所有 `.c/.cpp` 为 `<lib> 的 `lib<name>.a`，C++ 库自动追加 `c++/c++abi` 运行时链接
4. **自愈**：重新分析后自动合并旧配置里的人工项（`libraries/include_path/library_path`），手写/补的链接库不会丢

### C++ 自动桥接规则

C++ 头文件无法被 Kaula 生成的 C 代码直接引用，编译器自动生成 `extern "C"` 桥接。导出的函数一律使用 `kbridge_<函数名>` 前缀（避免与平台头冲突，如 windows.h 的 `GetVersion`）：

| C++ 类型 | 导出类型 | stub 内还原 |
|---|---|---|
| `T&` / `T&&` | `T*`（基础类型）/ `void*` | `*p` / `(*((T*)p))` |
| 基础类型指针（`char*`、`float*`） | 原样导出 | — |
| 其他指针（`ImVec2*` 等） | `void*` | `(T*)p` |
| 含 `::`/`<>` 的指针 | `void*` | `(T*)p` |
| 非基础值类型、函数指针、数组 | 不导出 | — |

- 同名重载只保留"最友好"的一个签名（转换次数最少、参数最少）
- `operator` 重载等非法 C 标识符自动过滤
- 已知限制：类成员方法、模板值返回类型暂不自动暴露

### 库配置文件（自动生成）

自动分析生成的 `<name>.json` 记录了 `auto_generated: true`。可人工补充
`libraries`（系统库）、`include_path`、`library_path` 等字段，重分析时
`MergeLibrariesInto` 会把人工项合并保留：

```json
{
  "name": "imgui",
  "auto_generated": true,
  "functions": {
    "kbridge_GetVersion": { "args": [], "return": "const char*" }
  },
  "libraries": ["imgui", "c++", "c++abi", "d3d11", "dwmapi", "d3dcompiler"]
}
```

### 自动发现（实现）

```go
// 编译器加载阶段（配置缺失即分析、过期即重分析）
libraries, err := stdlib.LoadPkgLibraries("pkglib/")
for _, lib := range libraries {
    if stdlib.ConfigStale(libDir, lib.Name) { // 缺失/过期/登记头文件消失
        result, _ := stdlib.AnalyzePackage(libDir)
        stdlib.MergeLibrariesInto(libDir, result) // 合并人工链接项并写回
    }
}
```

## C 头文件分析

### 分析器功能

`analyzer.go` 使用 Clang 解析 C 头文件：

1. **函数提取**：提取所有函数声明
2. **类型解析**：解析 typedef 和结构体
3. **宏检测**：检测实现宏（如 `STB_IMAGE_IMPLEMENTATION`）
4. **导出宏**：检测常见的导出宏

### 分析流程

```go
// 分析包
result, err := stdlib.AnalyzePackage("pkglib/stb_image")

// 结果包含：
// - 函数签名
// - 头文件路径
// - 库文件路径
// - 实现宏
// - 导出宏

// 写入配置
stdlib.WriteConfig("pkglib/stb_image", result)
```

### Clang AST 解析

```go
// 使用 Clang 解析头文件
cmd := exec.Command("clang", "-Xclang", "-ast-dump=json", "-fsyntax-only", headerFile)
output, err := cmd.Output()

// 解析 JSON AST
var ast ClangASTNode
json.Unmarshal(output, &ast)

// 提取函数声明
functions := extractFunctions(ast)
```

### 类型规范化

```go
// 规范化 C 类型
func normalizeType(qualType string) string {
    // 移除调用约定: __cdecl, __stdcall, etc.
    // 移除 struct/enum/class 关键字
    // 展开 typedef（最多 10 次）
}
```

## 模块依赖

### 依赖解析

```json
// dependencies.json
{
  "std.io": ["std.memory"],
  "std.json": ["std.string"],
  "std.web": ["std.io", "std.string"]
}
```

### BFS 依赖展开

```go
// 解析模块依赖
func resolveModuleDependencies(module string, config *StdlibConfig) []string {
    // BFS 展开所有依赖
    // 返回需要包含的头文件列表
}
```

## 使用示例

### 导入标准库

```kaula
import std.io
import std.math
import std.container

fn main() {
    println("Hello, World!")
    float x = math_sqrt(2.0)
    Vector<int> v = vector_create()
}
```

### 导入 freestanding 无依赖库（`--freestanding` 模式）

```kaula
import freestanding.base
import freestanding.memory
import freestanding.string
import freestanding.math
import freestanding.io

fn main() {
    // 内存
    char* buf = as<char*>(fs_alloc(64))
    memset(buf, 65, 8)
    fs_free(buf)
    
    // 字符串
    char* s = fs_strdup("hello")
    char numbuf[32]
    fs_itoa(-9876, numbuf)
    fs_itoa_hex(0xCAFE, numbuf, true)
    
    // 数学
    math_gcd(48, 36)
    math_is_pow2(256)
    
    // I/O（托管模式需覆写 fs_output_putchar）
    println("Hello freestanding!")
    print_int(2026)
    print("printf: %d %s\n", 42, "test")
}
```

### 使用第三方库

```kaula
import stb_image
import imgui   // C++ 库：自动生成 kbridge_* 桥接

fn main() {
    void* img = stbi_load("texture.png", &width, &height, &channels, 4)
    stbi_image_free(img)

    println(kbridge_GetVersion())         // 1.92.9
}
```

### 常用命令

| 命令 | 作用 |
|---|---|
| `kaulac --build-pkglib <库名>` | 幂等指令：无配置先自动分析 → 过期自动重分析 → 构建归档（平时无需手动跑，import 时自动完成） |
| `kaulac --build-pkglib all` | 构建 pkglib 下全部库 |
| `kaulac --analyze-pkg <库名>` | 强制手动重新解析（Clang 重新提取签名并重写 `<库名>.json`，常用于头文件改动后手动刷新） |
| `kaulac --analyze-pkg-all` | 重新解析 pkglib 下全部库 |
| `kaulac --force-pkg` | 参与构建时强制重建归档（配合 `--build-pkglib` 使用） |
| `kaulac --pkglib <目录> [文件]` | 编译时优先使用指定目录作为 pkglib（`--skip-auto-pkg` 可关闭自动分析自愈） |

> **注意 `--analyze-pkg` 与 `--build-pkglib` 的区别**：
> `--analyze-pkg` 直接重写配置，不走 `MergeLibrariesInto`，会把旧配置里人工补充的链接库（如 imgui 的 `d3d11/dwmapi/d3dcompiler`）丢掉；
> `--build-pkglib` 在过期重分析时走 `MergeLibrariesInto` 保留人工项。
> 配置含人工链接项时，建议用 `--build-pkglib <库名>` 代替 `--analyze-pkg <库名>`。

### 自动分析入口（何时触发）

除手动命令外，编译器在以下时机自动完成分析/重分析，无需人工干预：

| 时机 | 触发条件 | 入口 |
|---|---|---|
| 加载配置 | 扫描 `pkglib/` 时发现库目录没有同名 `<库名>.json` | `LoadPkgLibraries` → `AnalyzePackage` + `WriteConfig` |
| import 解析 | `import 某库` 时配置中不存在该库，且 `pkglib/` 下有对应目录 | `tryAnalyzeMissingPackage`（按需分析并写配置） |
| 编译自愈 | 正在使用的库配置过期（`auto_generated=true` 且头/源码比配置新、或登记头文件消失） | `compileCCode` → `ConfigStale` 触发重分析 + `MergeLibrariesInto` |

其中"编译自愈"只针对**已 import（usedModules）**的库执行，未使用的库不会被强制重分析；`--skip-auto-pkg` 可关闭全部自动分析。

### 编译器处理

```go
// 1. 加载 stdlib.json
config := stdlib.LoadStdlibConfig("stdlib.json")

// 2. 解析 import 语句
for _, imp := range program.Imports {
    module := config.GetModule(imp.Module)
    // 添加头文件包含
    includes = append(includes, module.Header)
}

// 3. freestanding 模式：额外链接 freestanding 库
if compilerOptions.Freestanding {
    // 自动添加 freestanding 库链接
    linkerArgs = append(linkerArgs, "kaula_freestanding.lib") // Windows
    // 或 libkaula_freestanding.a (Linux/macOS)
    
    // freestanding 模块的头文件路径已在 stdlib.json 中配置
    // 编译器会自动解析 import freestanding.* 并包含对应头文件
}

// 4. 生成 C 代码
for _, include := range includes {
    fmt.Fprintf(&buf, "#include %s\n", include)
}
```

## 配置管理

### 配置缓存

```go
// 线程安全的配置缓存
var configCache sync.Map

func LoadStdlibConfig(path string) (*StdlibConfig, error) {
    // 检查缓存
    if cached, ok := configCache.Load(path); ok {
        return cached.(*StdlibConfig), nil
    }
    
    // 加载配置
    config, err := loadConfig(path)
    if err != nil {
        return nil, err
    }
    
    // 缓存配置
    configCache.Store(path, config)
    return config, nil
}
```

### 配置验证

```go
// 验证配置完整性
func ValidateConfig(config *StdlibConfig) []string {
    var errors []string
    
    for name, module := range config.Modules {
        // 检查头文件是否存在
        if _, err := os.Stat(module.Header); os.IsNotExist(err) {
            errors = append(errors, fmt.Sprintf("header not found: %s", module.Header))
        }
        
        // 检查函数签名
        for fnName, fn := range module.Functions {
            if len(fn.Args) == 0 && !fn.Varargs {
                // 警告：无参数函数
            }
        }
    }
    
    return errors
}
```

## 最佳实践

1. **保持配置同步**：修改标准库后更新 `stdlib.json`
2. **验证第三方库**：确保 `pkglib/` 中的库配置正确
3. **使用自动分析**：为新库生成初始配置
4. **检查依赖**：确保模块依赖关系正确
5. **版本控制**：将 `stdlib.json` 纳入版本控制

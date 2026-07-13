# 标准库集成

编译器通过 `stdlib.json` 管理标准库函数签名，支持自动发现第三方库。实现位于 `internal/stdlib/` 目录。

当前标准库包含 **58 个模块**，超过 **800+** 个函数。

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

```go
type Module struct {
    Name      string              // 模块名
    Header    string              // 头文件路径
    Functions map[string]*Function // 函数映射
}
```

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

### pkglib 目录结构

```
pkglib/
├── stb_image/
│   ├── stb_image.h
│   ├── stb_image.json
│   └── stb_image.lib
├── nuklear/
│   ├── nuklear.h
│   ├── nuklear.json
│   └── nuklear.lib
└── zlib/
    ├── zlib.h
    ├── zlib.json
    └── zlib.lib
```

### 库配置文件

```json
{
  "name": "stb_image",
  "headers": ["\"stb_image.h\""],
  "libraries": ["stb_image.lib"],
  "functions": {
    "stbi_load": {
      "args": ["const char*", "int*", "int*", "int*", "int"],
      "return": "void*"
    },
    "stbi_image_free": {
      "args": ["void*"],
      "return": "void"
    }
  }
}
```

### 自动发现

```go
// 加载第三方库
libraries, err := stdlib.LoadPkgLibraries("pkglib/")

// 自动分析未配置的库
for _, lib := range libraries {
    if lib.Config == nil {
        // 使用 Clang 分析头文件
        stdlib.AnalyzePackage(lib.Dir)
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

### 使用第三方库

```kaula
import stb_image

fn main() {
    void* img = stbi_load("texture.png", &width, &height, &channels, 4)
    stbi_image_free(img)
}
```

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

// 3. 生成 C 代码
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

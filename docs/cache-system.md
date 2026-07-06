# 增量编译缓存系统

缓存系统实现增量编译，避免重复编译未变化的源文件。实现位于 `internal/cache/cache.go`。

## 设计

- **基于 SHA-256**：使用源文件哈希验证缓存有效性
- **原子写入**：先写临时文件再重命名，确保数据完整性
- **清单管理**：记录所有缓存条目元数据
- **自动清理**：支持按时间和大小清理过期缓存

## 缓存结构

```
cache/
├── manifest.json           # 缓存清单
├── main.c                  # 缓存的 C 代码
├── utils.c                 # 缓存的 C 代码
└── ...
```

## 缓存条目

```go
type CacheEntry struct {
    SourcePath     string    // 源文件路径
    SourceHash     string    // SHA-256 哈希
    SourceSize     int64     // 文件大小
    CCodePath      string    // C 代码路径
    Timestamp      time.Time // 缓存时间
    CompilerVersion string   // 编译器版本
    UsedModules    []string  // 使用的模块
}
```

## 缓存清单

```go
type CacheManifest struct {
    Entries    map[string]*CacheEntry  // 缓存条目
    Version    string                  // 清单版本
    UpdatedAt  time.Time              // 更新时间
}
```

## 缓存验证

### 验证流程

```
1. 加载缓存条目
2. 验证编译器版本
3. 计算源文件 SHA-256
4. 比对哈希值
5. 检查文件大小
6. 返回验证结果
```

### 验证条件

缓存命中需要同时满足：
- 编译器版本匹配
- 源文件哈希匹配
- 文件大小匹配

### 缓存失效

以下情况会导致缓存失效：
- 编译器版本变化
- 源文件内容修改
- 文件大小变化
- 手动清除缓存

## 缓存操作

### 检查缓存

```go
func (cm *CacheManager) Check(sourcePath, sourceData string) *CacheResult {
    // 1. 加载清单
    // 2. 查找条目
    // 3. 验证哈希
    // 4. 验证版本
    // 5. 返回结果
}
```

### 存储缓存

```go
func (cm *CacheManager) Store(sourcePath, sourceData, cCode string, usedModules []string) error {
    // 1. 计算哈希
    // 2. 创建临时文件
    // 3. 写入 C 代码
    // 4. 原子重命名
    // 5. 更新清单
    // 6. 保存清单
}
```

### 清理缓存

```go
// 按时间清理
func (cm *CacheManager) Clean(maxAge time.Duration, maxSize int64) error {
    // 1. 遍历条目
    // 2. 检查年龄
    // 3. 检查大小
    // 4. 删除过期条目
    // 5. 更新清单
}

// 清空所有缓存
func (cm *CacheManager) Purge() error {
    // 1. 删除所有缓存文件
    // 2. 清空清单
}
```

### 获取统计

```go
type CacheStats struct {
    TotalEntries  int       // 条目总数
    TotalSize     int64     // 总大小
    OldestEntry   time.Time // 最旧条目
    NewestEntry   time.Time // 最新条目
}

func (cm *CacheManager) GetStats() *CacheStats {
    // 统计缓存信息
}
```

## 缓存键

缓存键基于源文件基本名（去掉 `.kl` 扩展名）：

```
main.kl      → cache/main.c
src/app.kl   → cache/app.c
lib/utils.kl → cache/utils.c
```

## 原子写入

确保缓存写入的原子性：

```go
// 1. 创建临时文件
tmpFile := cCodePath + ".tmp"

// 2. 写入内容
os.WriteFile(tmpFile, data, 0644)

// 3. 原子重命名
os.Rename(tmpFile, cCodePath)
```

这防止了写入中断导致的缓存损坏。

## 缓存统计

```bash
# 查看缓存统计
$ kaulac.exe --cache-stats

=== Cache Statistics ===
Total entries: 5
Total size: 12.5 KB (0.01 MB)
Oldest entry: 2026-04-26 18:32:08
Newest entry: 2026-04-26 19:15:22
```

## 缓存管理命令

```bash
# 禁用缓存
kaulac.exe --no-cache program.kl

# 清理过期缓存（7 天以上）
kaulac.exe --clean-cache

# 清空所有缓存
kaulac.exe --purge-cache

# 查看缓存统计
kaulac.exe --cache-stats
```

## 缓存目录

默认缓存目录为工作目录下的 `cache/`：

```
project/
├── src/
│   └── main.kl
├── cache/              # 缓存目录
│   ├── manifest.json
│   └── main.c
└── main.exe
```

## 性能影响

### 首次编译

```
源文件 → 词法分析 → 语法分析 → 语义分析 → 代码生成 → Clang 编译
       ↓ 存储缓存
```

### 缓存命中

```
源文件 → 验证缓存 → 使用缓存的 C 代码 → Clang 编译
       ↓ 跳过词法/语法/语义/代码生成
```

典型加速：~2.9s → ~2.6s（节省 ~300ms）

## 注意事项

1. **缓存目录**：确保 `cache/` 目录可写
2. **版本控制**：建议将 `cache/` 加入 `.gitignore`
3. **磁盘空间**：定期清理过期缓存
4. **编译器更新**：更新编译器后缓存会自动失效

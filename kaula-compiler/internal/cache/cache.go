package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheEntry 表示单个文件的缓存条目
type CacheEntry struct {
	// 源文件路径
	SourcePath string `json:"source_path"`
	// 源文件哈希
	SourceHash string `json:"source_hash"`
	// 源文件大小
	SourceSize int64 `json:"source_size"`
	// 源文件修改时间
	SourceModTime time.Time `json:"source_mod_time"`
	// 生成的 C 代码哈希
	CCodeHash string `json:"ccode_hash"`
	// C 代码大小
	CCodeSize int64 `json:"ccode_size"`
	// 缓存创建时间
	CreatedAt time.Time `json:"created_at"`
	// 最后访问时间
	LastAccessed time.Time `json:"last_accessed"`
	// 使用的标准库模块
	UsedModules []string `json:"used_modules"`
	// 编译器版本
	CompilerVersion string `json:"compiler_version"`
}

// CacheManifest 表示缓存目录的清单文件
type CacheManifest struct {
	// 缓存格式版本
	Version int `json:"version"`
	// 所有缓存条目
	Entries map[string]*CacheEntry `json:"entries"`
	// 最后清理时间
	LastCleanup time.Time `json:"last_cleanup"`
	// 总缓存大小（字节）
	TotalSize int64 `json:"total_size"`
}

// CacheManager 管理编译缓存
type CacheManager struct {
	// 缓存目录路径
	CacheDir string
	// 缓存清单
	Manifest *CacheManifest
	// 当前编译器版本
	CompilerVersion string
}

// CacheResult 缓存查询结果
type CacheResult struct {
	// 是否命中缓存
	Hit bool
	// 缓存的 C 代码路径
	CCodePath string
	// 缓存条目
	Entry *CacheEntry
}

// NewCacheManager 创建缓存管理器
func NewCacheManager(cacheDir string, compilerVersion string) (*CacheManager, error) {
	// 确保缓存目录存在
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	manager := &CacheManager{
		CacheDir:        cacheDir,
		CompilerVersion: compilerVersion,
		Manifest: &CacheManifest{
			Version:     1,
			Entries:     make(map[string]*CacheEntry),
			LastCleanup: time.Now(),
			TotalSize:   0,
		},
	}

	// 加载现有清单
	if err := manager.loadManifest(); err != nil {
		// 如果加载失败但文件不存在，创建新清单
		if !os.IsNotExist(err) {
			fmt.Printf("Warning: Failed to load cache manifest: %v, creating new one\n", err)
		}
	}

	return manager, nil
}

// loadManifest 加载缓存清单
func (cm *CacheManager) loadManifest() error {
	manifestPath := filepath.Join(cm.CacheDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, cm.Manifest); err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	return nil
}

// saveManifest 保存缓存清单
func (cm *CacheManager) saveManifest() error {
	manifestPath := filepath.Join(cm.CacheDir, "manifest.json")
	data, err := json.MarshalIndent(cm.Manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// 原子写入（先写临时文件再重命名）
	tmpPath := manifestPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := os.Rename(tmpPath, manifestPath); err != nil {
		return fmt.Errorf("failed to rename manifest: %w", err)
	}

	return nil
}

// computeHash 计算文件内容的 SHA256 哈希
func computeHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// GetCacheKey 生成缓存键（基于源文件路径）
func (cm *CacheManager) GetCacheKey(sourcePath string) string {
	// 使用源文件的绝对路径作为键
	absPath, _ := filepath.Abs(sourcePath)
	// 将路径分隔符替换为下划线，避免 Windows 路径问题
	cacheKey := filepath.Base(absPath)
	return cacheKey[:len(cacheKey)-3] // 去掉 .kl 扩展名
}

// Check 检查缓存是否有效
func (cm *CacheManager) Check(sourcePath string, sourceData []byte) *CacheResult {
	cacheKey := cm.GetCacheKey(sourcePath)
	cCodePath := filepath.Join(cm.CacheDir, cacheKey+".c")
	metaPath := filepath.Join(cm.CacheDir, cacheKey+".meta.json")

	// 检查 C 代码文件是否存在
	if _, err := os.Stat(cCodePath); os.IsNotExist(err) {
		return &CacheResult{Hit: false, CCodePath: cCodePath}
	}

	// 检查元数据文件是否存在
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		return &CacheResult{Hit: false, CCodePath: cCodePath}
	}

	// 加载元数据
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return &CacheResult{Hit: false, CCodePath: cCodePath}
	}

	var entry CacheEntry
	if err := json.Unmarshal(metaData, &entry); err != nil {
		return &CacheResult{Hit: false, CCodePath: cCodePath}
	}

	// 验证编译器版本
	if entry.CompilerVersion != cm.CompilerVersion {
		fmt.Printf("[Cache] Compiler version mismatch, rebuilding\n")
		return &CacheResult{Hit: false, CCodePath: cCodePath}
	}

	// 计算当前源文件哈希
	currentHash := computeHash(sourceData)

	// 验证源文件哈希
	if currentHash != entry.SourceHash {
		fmt.Printf("[Cache] Source hash mismatch, rebuilding\n")
		return &CacheResult{Hit: false, CCodePath: cCodePath}
	}

	// 验证源文件大小
	currentSize := int64(len(sourceData))
	if currentSize != entry.SourceSize {
		fmt.Printf("[Cache] Source size mismatch, rebuilding\n")
		return &CacheResult{Hit: false, CCodePath: cCodePath}
	}

	// 缓存命中！
	fmt.Printf("[Cache] Cache hit for %s\n", sourcePath)
	entry.LastAccessed = time.Now()

	// 更新元数据中的访问时间
	cm.Manifest.Entries[cacheKey] = &entry
	if err := cm.saveEntryMeta(cacheKey, &entry); err != nil {
		fmt.Printf("[Cache] Warning: Failed to update meta: %v\n", err)
	}

	return &CacheResult{
		Hit:       true,
		CCodePath: cCodePath,
		Entry:     &entry,
	}
}

// Store 存储编译结果到缓存
func (cm *CacheManager) Store(sourcePath string, sourceData []byte, cCode string, usedModules []string) error {
	cacheKey := cm.GetCacheKey(sourcePath)
	cCodePath := filepath.Join(cm.CacheDir, cacheKey+".c")

	cCodeData := []byte(cCode)
	sourceHash := computeHash(sourceData)
	cCodeHash := computeHash(cCodeData)

	entry := &CacheEntry{
		SourcePath:    sourcePath,
		SourceHash:    sourceHash,
		SourceSize:    int64(len(sourceData)),
		SourceModTime: time.Now(),
		CCodeHash:     cCodeHash,
		CCodeSize:     int64(len(cCodeData)),
		CreatedAt:     time.Now(),
		LastAccessed:  time.Now(),
		UsedModules:   usedModules,
		CompilerVersion: cm.CompilerVersion,
	}

	// 原子写入 C 代码文件
	tmpPath := cCodePath + ".tmp"
	if err := os.WriteFile(tmpPath, cCodeData, 0644); err != nil {
		return fmt.Errorf("failed to write C code cache: %w", err)
	}
	if err := os.Rename(tmpPath, cCodePath); err != nil {
		return fmt.Errorf("failed to rename C code cache: %w", err)
	}

	// 保存元数据
	if err := cm.saveEntryMeta(cacheKey, entry); err != nil {
		return fmt.Errorf("failed to save entry meta: %w", err)
	}

	// 更新清单
	cm.Manifest.Entries[cacheKey] = entry
	cm.Manifest.TotalSize += entry.CCodeSize + entry.SourceSize

	if err := cm.saveManifest(); err != nil {
		fmt.Printf("[Cache] Warning: Failed to save manifest: %v\n", err)
	}

	fmt.Printf("[Cache] Stored cache for %s (%d bytes)\n", sourcePath, len(cCodeData))
	return nil
}

// saveEntryMeta 保存条目元数据
func (cm *CacheManager) saveEntryMeta(cacheKey string, entry *CacheEntry) error {
	metaPath := filepath.Join(cm.CacheDir, cacheKey+".meta.json")
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, metaPath)
}

// Clean 清理缓存
func (cm *CacheManager) Clean(maxAge time.Duration, maxSize int64) error {
	fmt.Printf("[Cache] Cleaning cache (maxAge=%v, maxSize=%d bytes)\n", maxAge, maxSize)

	now := time.Now()
	removedCount := 0
	removedSize := int64(0)

	// 遍历所有条目
	for cacheKey, entry := range cm.Manifest.Entries {
		shouldRemove := false

		// 检查年龄
		if now.Sub(entry.LastAccessed) > maxAge {
			fmt.Printf("[Cache] Removing %s (age > %v)\n", cacheKey, maxAge)
			shouldRemove = true
		}

		// 检查总大小（如果超过限制，删除最旧的）
		if cm.Manifest.TotalSize > maxSize && !shouldRemove {
			shouldRemove = true
			fmt.Printf("[Cache] Removing %s (cache too large)\n", cacheKey)
		}

		if shouldRemove {
			// 删除 C 代码文件
			cCodePath := filepath.Join(cm.CacheDir, cacheKey+".c")
			os.Remove(cCodePath)

			// 删除元数据文件
			metaPath := filepath.Join(cm.CacheDir, cacheKey+".meta.json")
			os.Remove(metaPath)

			// 从清单中移除
			removedSize += entry.CCodeSize + entry.SourceSize
			delete(cm.Manifest.Entries, cacheKey)
			removedCount++
		}
	}

	// 更新清单
	cm.Manifest.TotalSize -= removedSize
	cm.Manifest.LastCleanup = now

	if removedCount > 0 {
		fmt.Printf("[Cache] Cleaned %d entries, freed %d bytes\n", removedCount, removedSize)
		if err := cm.saveManifest(); err != nil {
			return fmt.Errorf("failed to save manifest after cleanup: %w", err)
		}
	}

	return nil
}

// GetStats 获取缓存统计信息
func (cm *CacheManager) GetStats() (totalEntries int, totalSize int64, oldestEntry time.Time, newestEntry time.Time) {
	totalEntries = len(cm.Manifest.Entries)
	totalSize = cm.Manifest.TotalSize

	if totalEntries == 0 {
		return
	}

	oldestEntry = time.Now()
	newestEntry = time.Time{}

	for _, entry := range cm.Manifest.Entries {
		if entry.CreatedAt.Before(oldestEntry) {
			oldestEntry = entry.CreatedAt
		}
		if entry.CreatedAt.After(newestEntry) {
			newestEntry = entry.CreatedAt
		}
	}

	return
}

// ListEntries 列出所有缓存条目
func (cm *CacheManager) ListEntries() []*CacheEntry {
	entries := make([]*CacheEntry, 0, len(cm.Manifest.Entries))
	for _, entry := range cm.Manifest.Entries {
		entries = append(entries, entry)
	}
	return entries
}

// Remove 移除指定文件的缓存
func (cm *CacheManager) Remove(sourcePath string) error {
	cacheKey := cm.GetCacheKey(sourcePath)

	// 删除 C 代码文件
	cCodePath := filepath.Join(cm.CacheDir, cacheKey+".c")
	if err := os.Remove(cCodePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove C code: %w", err)
	}

	// 删除元数据文件
	metaPath := filepath.Join(cm.CacheDir, cacheKey+".meta.json")
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove meta: %w", err)
	}

	// 从清单中移除
	if entry, ok := cm.Manifest.Entries[cacheKey]; ok {
		cm.Manifest.TotalSize -= entry.CCodeSize + entry.SourceSize
		delete(cm.Manifest.Entries, cacheKey)
	}

	if err := cm.saveManifest(); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	fmt.Printf("[Cache] Removed cache for %s\n", sourcePath)
	return nil
}

// Purge 清空整个缓存
func (cm *CacheManager) Purge() error {
	fmt.Printf("[Cache] Purging all cache entries\n")

	// 删除所有 .c 和 .meta.json 文件
	entries, err := os.ReadDir(cm.CacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) == ".c" || filepath.Ext(name) == ".json" {
			filePath := filepath.Join(cm.CacheDir, name)
			if err := os.Remove(filePath); err != nil {
				fmt.Printf("[Cache] Warning: Failed to remove %s: %v\n", name, err)
			}
		}
	}

	// 重置清单
	cm.Manifest.Entries = make(map[string]*CacheEntry)
	cm.Manifest.TotalSize = 0
	cm.Manifest.LastCleanup = time.Now()

	if err := cm.saveManifest(); err != nil {
		return fmt.Errorf("failed to save manifest after purge: %w", err)
	}

	fmt.Printf("[Cache] Cache purged successfully\n")
	return nil
}

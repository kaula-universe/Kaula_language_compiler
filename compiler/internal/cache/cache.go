// Package cache 提供增量编译缓存：基于源文件内容哈希 + 被 import 文件哈希
// 作为缓存键，命中时直接复用已生成的 .c 文件，跳过 codegen + C 编译。
//
// 缓存布局（cacheDir 下）：
//
//	<key>.c    生成的 C 源码
//	<key>.meta  JSON 清单（版本 / 源哈希 / 导入哈希 / 模块 / 时间戳）
//
// <key> = SHA256(输入文件绝对路径)，文件名安全。
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CacheManager 管理单个缓存目录。
type CacheManager struct {
	dir     string
	version string
}

// CacheResult 是 Check 的返回值。
type CacheResult struct {
	Hit       bool
	CCodePath string
}

// entryMeta 是每个缓存条目的清单。
type entryMeta struct {
	Version      string            `json:"version"`
	SourceHash   string            `json:"source_hash"`
	ImportHashes map[string]string `json:"import_hashes,omitempty"`
	UsedModules  []string          `json:"used_modules,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

// HashContent 返回 data 的 SHA-256 十六进制摘要。
func HashContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// NewCacheManager 初始化缓存管理器，确保 cacheDir 存在。
func NewCacheManager(cacheDir, version string) (*CacheManager, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	return &CacheManager{dir: cacheDir, version: version}, nil
}

// GetCacheKey 返回输入文件路径对应的稳定缓存键（文件名安全）。
func (m *CacheManager) GetCacheKey(inputFile string) string {
	abs, err := filepath.Abs(inputFile)
	if err != nil {
		abs = inputFile
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])
}

func (m *CacheManager) cCodePath(key string) string { return filepath.Join(m.dir, key+".c") }
func (m *CacheManager) metaPath(key string) string  { return filepath.Join(m.dir, key+".meta") }

// Check 校验缓存是否命中：清单存在、版本/源哈希/导入哈希全部匹配，且 .c 文件存在。
// 无论命中与否，CCodePath 都指向该条目的 .c 路径（未命中时为待写入路径）。
func (m *CacheManager) Check(inputFile string, data []byte, importFileHashes map[string]string) CacheResult {
	key := m.GetCacheKey(inputFile)
	cpath := m.cCodePath(key)

	raw, err := os.ReadFile(m.metaPath(key))
	if err != nil {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	var meta entryMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	if meta.Version != m.version {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	if meta.SourceHash != HashContent(data) {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	if !hashMapsEqual(meta.ImportHashes, importFileHashes) {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	if _, err := os.Stat(cpath); err != nil {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	return CacheResult{Hit: true, CCodePath: cpath}
}

// Store 写入生成的 C 代码与清单。Store 总是覆盖已有条目。
func (m *CacheManager) Store(inputFile string, data []byte, output string, usedModules []string, importFileHashes map[string]string) error {
	key := m.GetCacheKey(inputFile)
	if err := os.WriteFile(m.cCodePath(key), []byte(output), 0o644); err != nil {
		return err
	}
	meta := entryMeta{
		Version:      m.version,
		SourceHash:   HashContent(data),
		ImportHashes: importFileHashes,
		UsedModules:  usedModules,
		CreatedAt:    time.Now().UTC(),
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(m.metaPath(key), raw, 0o644)
}

// GetStats 返回 (条目数, .c 总字节, 最早/最近创建时间)。
func (m *CacheManager) GetStats() (int, int64, time.Time, time.Time) {
	entries := m.listEntries()
	if len(entries) == 0 {
		return 0, 0, time.Time{}, time.Time{}
	}
	var totalSize int64
	var oldest, newest time.Time
	for _, e := range entries {
		totalSize += e.cSize
		if oldest.IsZero() || e.createdAt.Before(oldest) {
			oldest = e.createdAt
		}
		if newest.IsZero() || e.createdAt.After(newest) {
			newest = e.createdAt
		}
	}
	return len(entries), totalSize, oldest, newest
}

// Clean 清理：先删除超过 maxAge 的条目，再若总大小超过 maxSize 则按最旧优先淘汰。
func (m *CacheManager) Clean(maxAge time.Duration, maxSize int64) error {
	entries := m.listEntries()
	now := time.Now().UTC()
	var survivors []cacheEntry
	var totalSize int64
	for _, e := range entries {
		if maxAge > 0 && now.Sub(e.createdAt) > maxAge {
			m.removeEntry(e.key)
			continue
		}
		survivors = append(survivors, e)
		totalSize += e.cSize
	}
	if maxSize <= 0 || totalSize <= maxSize {
		return nil
	}
	// 按时间从旧到新排序，优先淘汰最旧
	sort.Slice(survivors, func(i, j int) bool {
		return survivors[i].createdAt.Before(survivors[j].createdAt)
	})
	for _, e := range survivors {
		if totalSize <= maxSize {
			break
		}
		m.removeEntry(e.key)
		totalSize -= e.cSize
	}
	return nil
}

// Purge 清空整个缓存目录后重建。
func (m *CacheManager) Purge() error {
	if err := os.RemoveAll(m.dir); err != nil {
		return err
	}
	return os.MkdirAll(m.dir, 0o755)
}

// cacheEntry 是扫描目录得到的单条缓存信息。
type cacheEntry struct {
	key       string
	createdAt time.Time
	cSize     int64
}

// listEntries 扫描目录，返回所有合法缓存条目。
func (m *CacheManager) listEntries() []cacheEntry {
	dir, err := os.Open(m.dir)
	if err != nil {
		return nil
	}
	names, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		return nil
	}
	var out []cacheEntry
	for _, name := range names {
		key, ok := stripSuffix(name, ".meta")
		if !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(m.dir, name))
		if err != nil {
			continue
		}
		var meta entryMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		var cSize int64
		if fi, err := os.Stat(m.cCodePath(key)); err == nil {
			cSize = fi.Size()
		}
		out = append(out, cacheEntry{key: key, createdAt: meta.CreatedAt, cSize: cSize})
	}
	return out
}

func (m *CacheManager) removeEntry(key string) {
	_ = os.Remove(m.cCodePath(key))
	_ = os.Remove(m.metaPath(key))
}

// stripSuffix 返回去后缀后的 name 及是否匹配。
func stripSuffix(name, suffix string) (string, bool) {
	if len(name) <= len(suffix) {
		return "", false
	}
	if name[len(name)-len(suffix):] != suffix {
		return "", false
	}
	return name[:len(name)-len(suffix)], true
}

// hashMapsEqual 比较 map[string]string，nil 与空 map 视作相等。
func hashMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
